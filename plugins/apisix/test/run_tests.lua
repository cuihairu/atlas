-- Plugin tests for plugins/apisix (plain Lua 5.4, no OpenResty required).
--
--   lua test/run_tests.lua
--
-- Each case installs fresh mocks, loads the plugin, drives the access
-- phase, and asserts on injected headers / short-circuit exits.

local script_dir = arg and arg[0]:match("(.*/)") or "./"
local mock = dofile(script_dir .. "mock_apisix.lua")

local passed, failed = 0, 0

local function assert_eq(actual, expected, msg)
    if actual ~= expected then
        error(string.format("%s: expected %s, got %s",
            msg or "assert", tostring(expected), tostring(actual)), 2)
    end
end

local function run(name, fn)
    local ok, err = pcall(fn)
    if ok then
        passed = passed + 1
        print("PASS  " .. name)
    else
        failed = failed + 1
        print("FAIL  " .. name .. "\n      " .. tostring(err))
    end
end

local function load(plugin)
    return mock.load_plugin(script_dir .. "../" .. plugin)
end

-- ── atlas-auth ──────────────────────────────────────────────────

run("auth: valid token injects account header", function()
    mock.install()
    local p = load("atlas-auth.lua")
    local ctx = mock.ctx({ headers = { ["X-Player-Token"] = "tok-42" } })
    local exited = mock.capture_exit(p.access, {
        token_header = "X-Player-Token",
        account_header = "X-Atlas-Player-ID",
        accounts = { ["tok-42"] = 42 },
        required = true,
    }, ctx)
    assert_eq(exited, nil, "should not exit")
    assert_eq(ctx.injected["X-Atlas-Player-ID"], "42", "injected account")
end)

run("auth: missing token with required=true exits 401", function()
    mock.install()
    local p = load("atlas-auth.lua")
    local ctx = mock.ctx({ headers = {} })
    local exited = mock.capture_exit(p.access, {
        token_header = "X-Player-Token",
        account_header = "X-Atlas-Player-ID",
        accounts = { ["tok-42"] = 42 },
        required = true,
    }, ctx)
    assert_eq(exited and exited.code, 401, "exit code")
end)

run("auth: unknown token exits 401", function()
    mock.install()
    local p = load("atlas-auth.lua")
    local ctx = mock.ctx({ headers = { ["X-Player-Token"] = "nope" } })
    local exited = mock.capture_exit(p.access, {
        token_header = "X-Player-Token",
        account_header = "X-Atlas-Player-ID",
        accounts = { ["tok-42"] = 42 },
        required = true,
    }, ctx)
    assert_eq(exited and exited.code, 401, "exit code")
end)

run("auth: required=false passes anonymous through", function()
    mock.install()
    local p = load("atlas-auth.lua")
    local ctx = mock.ctx({ headers = {} })
    local exited = mock.capture_exit(p.access, {
        token_header = "X-Player-Token",
        account_header = "X-Atlas-Player-ID",
        accounts = { ["tok-42"] = 42 },
        required = false,
    }, ctx)
    assert_eq(exited, nil, "should not exit")
    assert_eq(ctx.injected["X-Atlas-Player-ID"], nil, "no account injected")
end)

run("auth: accepts Authorization Bearer", function()
    mock.install()
    local p = load("atlas-auth.lua")
    local ctx = mock.ctx({ headers = { ["Authorization"] = "Bearer tok-7" } })
    local exited = mock.capture_exit(p.access, {
        token_header = "X-Player-Token",
        account_header = "X-Atlas-Player-ID",
        accounts = { ["tok-7"] = 7 },
        required = true,
    }, ctx)
    assert_eq(exited, nil, "should not exit")
    assert_eq(ctx.injected["X-Atlas-Player-ID"], "7", "injected account from bearer")
end)

run("auth: strip_token clears the player header", function()
    mock.install()
    local p = load("atlas-auth.lua")
    local ctx = mock.ctx({ headers = { ["X-Player-Token"] = "tok-42" } })
    p.access({
        token_header = "X-Player-Token",
        account_header = "X-Atlas-Player-ID",
        accounts = { ["tok-42"] = 42 },
        required = true,
        strip_token = true,
    }, ctx)
    assert_eq(mock.state.last_cleared, "X-Player-Token", "header cleared")
end)

-- ── atlas-ratelimit ─────────────────────────────────────────────

local rl_conf = {
    groups = {
        { prefix = "/v1/discovery", rate = 3 },
        { prefix = "/v1/registry", rate = 1 },
    },
    default_rate = 0,
    rejected_code = 429,
}

run("ratelimit: allows traffic under the quota", function()
    mock.install()
    local p = load("atlas-ratelimit.lua")
    for i = 1, 3 do
        local ctx = mock.ctx({ uri = "/v1/discovery/servers" })
        local exited = mock.capture_exit(p.access, rl_conf, ctx)
        assert_eq(exited, nil, "request " .. i .. " should pass")
    end
end)

run("ratelimit: rejects over the quota with 429", function()
    mock.install()
    local p = load("atlas-ratelimit.lua")
    for _ = 1, 3 do
        mock.capture_exit(p.access, rl_conf, mock.ctx({ uri = "/v1/discovery/servers" }))
    end
    local exited = mock.capture_exit(p.access, rl_conf, mock.ctx({ uri = "/v1/discovery/servers" }))
    assert_eq(exited and exited.code, 429, "4th request in window")
end)

run("ratelimit: longest prefix wins", function()
    mock.install()
    local p = load("atlas-ratelimit.lua")
    local conf = {
        groups = {
            { prefix = "/v1", rate = 10 },
            { prefix = "/v1/registry", rate = 1 },
        },
        default_rate = 0,
    }
    mock.capture_exit(p.access, conf, mock.ctx({ uri = "/v1/registry/servers/s1/heartbeat" }))
    local exited = mock.capture_exit(p.access, conf, mock.ctx({ uri = "/v1/registry/servers/s1/heartbeat" }))
    assert_eq(exited and exited.code, 429, "registry group quota is 1")
end)

run("ratelimit: other clients are unaffected", function()
    mock.install()
    local p = load("atlas-ratelimit.lua")
    for _ = 1, 3 do
        mock.capture_exit(p.access, rl_conf, mock.ctx({ uri = "/v1/discovery/servers", remote_addr = "10.0.0.1" }))
    end
    local ctx = mock.ctx({ uri = "/v1/discovery/servers", remote_addr = "10.0.0.2" })
    local exited = mock.capture_exit(p.access, rl_conf, ctx)
    assert_eq(exited, nil, "second client passes")
end)

run("ratelimit: default_rate applies to unmatched URIs", function()
    mock.install()
    local p = load("atlas-ratelimit.lua")
    local conf = { groups = { { prefix = "/v1/registry", rate = 1 } }, default_rate = 2 }
    mock.capture_exit(p.access, conf, mock.ctx({ uri = "/v1/other" }))
    mock.capture_exit(p.access, conf, mock.ctx({ uri = "/v1/other" }))
    local exited = mock.capture_exit(p.access, conf, mock.ctx({ uri = "/v1/other" }))
    assert_eq(exited and exited.code, 429, "3rd unmatched request")
end)

run("ratelimit: default_rate=0 keeps unmatched URIs unlimited", function()
    mock.install()
    local p = load("atlas-ratelimit.lua")
    for _ = 1, 10 do
        local exited = mock.capture_exit(p.access, rl_conf, mock.ctx({ uri = "/v1/directory/characters" }))
        assert_eq(exited, nil, "unlimited group")
    end
end)

run("ratelimit: fails open without the shared dict", function()
    mock.install({ dict_enabled = false })
    local p = load("atlas-ratelimit.lua")
    local ctx = mock.ctx({ uri = "/v1/discovery/servers" })
    local exited = mock.capture_exit(p.access, rl_conf, ctx)
    assert_eq(exited, nil, "should fail open")
end)

print(string.format("\n%d passed, %d failed", passed, failed))
os.exit(failed == 0 and 0 or 1)
