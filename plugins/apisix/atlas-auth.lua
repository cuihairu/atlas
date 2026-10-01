--
-- atlas-auth: APISIX plugin for Atlas (TODO v0.1.13).
--
-- Validates the player token on requests routed to Atlas and injects the
-- player identity into headers Atlas understands. Typical deployment:
-- login service issues tokens → client calls APISIX → this plugin checks
-- the token and forwards X-Atlas-Player-ID to Atlas public endpoints.
--
-- Config:
--   token_header   header carrying the player token (default "X-Player-Token";
--                  "Authorization: Bearer <token>" is also accepted)
--   account_header header injected for Atlas (default "X-Atlas-Player-ID")
--   accounts       map of token → account id (static mapping; point at your
--                  token service for real deployments)
--   required       reject requests without a valid token (default true)
--   strip_token    remove the player token header before proxying (default false)
--
-- Returns 401 when required and the token is missing or unknown.
--

local core = require("apisix.core")

local plugin_name = "atlas-auth"

local schema = {
    type = "object",
    properties = {
        token_header = { type = "string", default = "X-Player-Token" },
        account_header = { type = "string", default = "X-Atlas-Player-ID" },
        accounts = {
            type = "object",
            additionalProperties = { type = {"string", "number"} },
        },
        required = { type = "boolean", default = true },
        strip_token = { type = "boolean", default = false },
    },
    required = {},
}

local _M = {
    version = 0.1,
    priority = 2500,
    name = plugin_name,
    schema = schema,
}

local function bearer_token(headers)
    local authz = headers["authorization"]
    if not authz then
        return nil
    end
    local tok = authz:match("[Bb]earer%s+(.+)")
    return tok
end

function _M.check(conf)
    return true
end

function _M.access(conf, ctx)
    local headers = core.request.headers(ctx)
    -- APISIX lowercases incoming header names; match against the lowered
    -- form so configurations like "X-Player-Token" keep working.
    local token = headers[(conf.token_header or ""):lower()] or bearer_token(headers)

    if not token then
        if conf.required then
            core.log.warn("atlas-auth: missing player token, rejecting")
            return core.response.exit(401, { message = "player token required" })
        end
        return
    end

    local account = conf.accounts and conf.accounts[token]
    if not account then
        if conf.required then
            core.log.warn("atlas-auth: unknown player token, rejecting")
            return core.response.exit(401, { message = "invalid player token" })
        end
        return
    end

    -- Inject the identity Atlas routes on (routing/account lookups).
    core.request.set_header(ctx, conf.account_header, tostring(account))

    if conf.strip_token then
        ngx.req.clear_header(conf.token_header)
    end
end

return _M
