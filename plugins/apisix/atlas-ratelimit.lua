--
-- atlas-ratelimit: APISIX plugin for Atlas (TODO v0.1.13).
--
-- Per-endpoint-group rate limiting for Atlas routes: one route can serve
-- the whole /v1 surface while each group (discovery / directory / routing /
-- registry) gets its own quota. Fixed-window counters live in the
-- "atlas_ratelimit" shared dict (declare it in APISIX config.yaml, see the
-- README).
--
-- Config:
--   groups        array of { prefix, rate, window? } — longest prefix wins.
--                 rate = requests per window (window seconds, default 60).
--   default_rate  quota for requests matching no group (0 = unlimited).
--   rejected_code status for rejected requests (default 429).
--
-- Counting key: group prefix + client IP, so one gateway can host both
-- generous discovery traffic and tight registry writes.
--

local core = require("apisix.core")

local plugin_name = "atlas-ratelimit"

local DEFAULT_WINDOW = 60

local schema = {
    type = "object",
    properties = {
        groups = {
            type = "array",
            items = {
                type = "object",
                properties = {
                    prefix = { type = "string" },
                    rate = { type = "integer", minimum = 1 },
                    window = { type = "integer", minimum = 1, default = DEFAULT_WINDOW },
                },
                required = { "prefix", "rate" },
            },
            default = {},
        },
        default_rate = { type = "integer", minimum = 0, default = 0 },
        rejected_code = { type = "integer", minimum = 400, maximum = 599, default = 429 },
    },
    required = {},
}

local _M = {
    version = 0.1,
    priority = 2400,
    name = plugin_name,
    schema = schema,
}

local function shared_dict()
    -- Declared via nginx_config.http.shared_dicts in APISIX config.yaml.
    local dict = ngx.shared and ngx.shared.atlas_ratelimit
    if not dict then
        core.log.error("atlas-ratelimit: shared dict 'atlas_ratelimit' is not declared; limiting disabled")
        return nil
    end
    return dict
end

local function match_group(conf, uri)
    local best
    for _, g in ipairs(conf.groups or {}) do
        if uri:sub(1, #g.prefix) == g.prefix then
            if not best or #g.prefix > #best.prefix then
                best = g
            end
        end
    end
    return best
end

function _M.check(conf)
    return true
end

function _M.access(conf, ctx)
    if (conf.default_rate or 0) == 0 and not next(conf.groups or {}) then
        return -- nothing configured: pass through
    end

    local dict = shared_dict()
    if not dict then
        return -- fail open; misconfiguration must not take the gateway down
    end

    local uri = ctx.var and ctx.var.uri or ngx.var.uri or ""
    local group = match_group(conf, uri)
    local rate, window
    if group then
        rate, window = group.rate, group.window or DEFAULT_WINDOW
    else
        rate, window = conf.default_rate or 0, DEFAULT_WINDOW
    end
    if rate == 0 then
        return -- unlimited group
    end

    local client = ctx.var and ctx.var.remote_addr or ngx.var.remote_addr or "-"
    local bucket = math.floor(ngx.now() / window)
    local key = string.format("%s:%s:%d", group and group.prefix or "/", client, bucket)

    local count = dict:incr(key, 1, 0, window)
    if count > rate then
        core.log.warn("atlas-ratelimit: limit exceeded uri=", uri, " key=", key, " count=", count)
        return core.response.exit(conf.rejected_code or 429, { message = "rate limit exceeded" })
    end
end

return _M
