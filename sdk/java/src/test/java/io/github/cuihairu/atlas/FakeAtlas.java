package io.github.cuihairu.atlas;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.InputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.regex.Pattern;

/** Minimal Atlas stand-in on a real socket: regex routes → (status, json). */
final class FakeAtlas implements AutoCloseable {

    record Recorded(String method, String path, Map<String, String> query,
                    Map<String, String> headers, String body) {}

    record Reply(int status, String json) {}

    interface Handler {
        Reply handle(Recorded req);
    }

    record Route(String method, Pattern pattern, Handler handler) {}

    final List<Recorded> requests = new CopyOnWriteArrayList<>();
    final List<Route> routes = new CopyOnWriteArrayList<>();

    private final HttpServer server;
    private final String baseUrl;

    FakeAtlas() throws IOException {
        this.server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        this.server.createContext("/", this::dispatch);
        this.server.start();
        this.baseUrl = "http://127.0.0.1:" + server.getAddress().getPort();
    }

    String url() {
        return baseUrl;
    }

    @Override
    public void close() {
        server.stop(0);
    }

    private void dispatch(HttpExchange ex) throws IOException {
        try (ex) {
            String body;
            try (InputStream in = ex.getRequestBody()) {
                body = new String(in.readAllBytes(), StandardCharsets.UTF_8);
            }
            String rawQuery = ex.getRequestURI().getRawQuery();
            Map<String, String> query = new HashMap<>();
            if (rawQuery != null && !rawQuery.isEmpty()) {
                for (String pair : rawQuery.split("&")) {
                    String[] kv = pair.split("=", 2);
                    if (kv.length == 2) {
                        query.put(kv[0], kv[1]);
                    }
                }
            }
            Map<String, String> headers = new HashMap<>();
            ex.getRequestHeaders().forEach((k, v) -> headers.put(k.toLowerCase(), String.join(",", v)));

            Recorded req = new Recorded(ex.getRequestMethod(), ex.getRequestURI().getPath(),
                    query, headers, body);
            requests.add(req);
            for (Route route : routes) {
                if (route.method().equals(req.method()) && route.pattern().matcher(req.path()).matches()) {
                    Reply reply = route.handler().handle(req);
                    byte[] data = reply.json().getBytes(StandardCharsets.UTF_8);
                    ex.getResponseHeaders().set("Content-Type", "application/json");
                    ex.sendResponseHeaders(reply.status(), data.length == 0 ? -1 : data.length);
                    if (data.length > 0) {
                        ex.getResponseBody().write(data);
                    }
                    return;
                }
            }
            ex.sendResponseHeaders(404, -1);
        }
    }

    Recorded request(String method, Pattern path) {
        return requests.stream()
                .filter(r -> r.method().equals(method) && path.matcher(r.path()).matches())
                .findFirst()
                .orElseThrow(() -> new AssertionError("no " + method + " " + path + " recorded"));
    }

    static Route route(String method, String pattern, Handler handler) {
        return new Route(method, Pattern.compile(pattern), handler);
    }

    static Reply reply(int status, String json) {
        return new Reply(status, json);
    }
}
