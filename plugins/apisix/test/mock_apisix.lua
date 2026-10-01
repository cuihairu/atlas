-- Minimal apisix.core/ngx mocks so the plugins can be exercised with plain
-- Lua 5.4 (CI has no OpenResty). Load this file before dofile()-ing a plugin.
--
-- Usage:
--   local mock = dofile("test/mock_apisix.lua")
--   local plugin = mock.load_plugin("atlas-auth")
--   local ctx = mock.ctx({ headers = { ["X-Player-Token"] = "t1" } })
--   local exit = mock.capture_exit(plugin.access, conf, ctx)

local mock = {}

-- ── request context ─────────────────────────────────────────────

-- ctx builds an APISIX-ish request context.
function mock.ctx(opts)
    opts = opts or {}
    return {
        headers = opts.headers or {},
        injected = {},
        cleared = {},
        var = {
            uri = opts.uri or "/v1/discovery/servers",
            remote_addr = opts.remote_addr or "10.0.0.9",
        },
    }
end

-- capture_exit runs fn and returns the exit sentinel if the plugin exited
-- short (nil otherwise).
function mock.capture_exit(fn, ...)
    local ok, err = pcall(fn, ...)
    if ok then
        return nil
    end
    if type(err) == "table" and err.atlas_exit then
        return err
    end
    error(err, 0) -- real error, re-raise
end

-- ── fake shared dict ────────────────────────────────────────────

local FakeDict = {}
FakeDict.__index = FakeDict

function mock.new_dict()
    return setmetatable({ counts = {} }, FakeDict)
end

function FakeDict:incr(key, delta, init, _expiry)
    self.counts[key] = (self.counts[key] or init or 0) + delta
    return self.counts[key]
end

function FakeDict:get(key)
    return self.counts[key]
end

-- ── apisix.core / ngx preload ───────────────────────────────────

-- install(opts) wires the mocks. Returns the installed state for asserts.
function mock.install(opts)
    opts = opts or {}
    local state = { logs = {}, dict = opts.dict or mock.new_dict() }

    local core = {
        request = {
            -- APISIX lowercases header names; mirror that here.
            headers = function(ctx)
                local out = {}
                for k, v in pairs(ctx.headers) do
                    out[k:lower()] = v
                end
                return out
            end,
            set_header = function(ctx, name, value) ctx.injected[name] = value end,
        },
        response = {
            exit = function(code, body)
                error({ atlas_exit = true, code = code, body = body }, 0)
            end,
        },
        log = {
            warn = function(...)
                local parts = {}
                for _, v in ipairs({ ... }) do parts[#parts + 1] = tostring(v) end
                state.logs[#state.logs + 1] = table.concat(parts, " ")
            end,
            error = function(...)
                local parts = {}
                for _, v in ipairs({ ... }) do parts[#parts + 1] = tostring(v) end
                state.logs[#state.logs + 1] = table.concat(parts, " ")
            end,
        },
    }
    package.preload["apisix.core"] = function() return core end

    ngx = ngx or {}
    ngx.req = { clear_header = function(name)
        -- clear_header needs request access; record for assertions.
        state.last_cleared = name
    end }
    ngx.now = function() return 1000000 end
    ngx.var = { uri = "", remote_addr = "10.0.0.9" }
    ngx.shared = { atlas_ratelimit = opts.dict_enabled == false and nil or state.dict }

    mock.state = state
    return state
end

-- load_plugin dofile()s a plugin with the mocks preloaded.
function mock.load_plugin(path)
    return dofile(path)
end

return mock
