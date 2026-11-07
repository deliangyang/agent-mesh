# nginx in front of Agent Mesh relay

When the relay sits behind nginx and nodes connect only through the reverse tunnel, configure streaming and WebSocket paths explicitly. Buffering or short read timeouts make `/v1/to/.../stream` and E2E chat appear stuck.

## Upstream

Point `proxy_pass` at the relay listen address (for example `127.0.0.1:8443`).

## E2E WebSocket (primary chat/catalog stream)

The web UI sends `reply`, `send`, `watch`, and catalog traffic over `GET /v1/to/{node_id}/e2e/ws` after upgrade.

```nginx
location ~ ^/v1/to/[^/]+/e2e/ws$ {
    proxy_pass http://agent_mesh_relay;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

## HTTP stream fallback and OpenAI-style SSE

Used by `ctl` and browser fallback when WebSocket is unavailable.

```nginx
location ~ ^/v1/to/[^/]+/stream$ {
    proxy_pass http://agent_mesh_relay;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_cache off;
    proxy_request_buffering off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
    chunked_transfer_encoding on;
}

location ~ ^/v1/to/[^/]+/events {
    proxy_pass http://agent_mesh_relay;
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_read_timeout 3600s;
}
```

The relay sets `X-Accel-Buffering: no` on streamed responses; keep `proxy_buffering off` on these locations as defense in depth.

## Rest of the relay API

```nginx
location / {
    proxy_pass http://agent_mesh_relay;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

## Node tunnel

Nodes open `GET /v1/node/tunnel` on the same relay host. That path must allow WebSocket upgrade (include it in a location that sets `Upgrade` / `Connection`, or reuse a prefix rule that covers `/v1/node/tunnel`).

```nginx
location = /v1/node/tunnel {
    proxy_pass http://agent_mesh_relay;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_buffering off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

## TLS termination

Terminate TLS on nginx and proxy to the relay over HTTP on localhost. Browsers use `wss:` when the page is loaded over `https:`; no extra relay flags are required.
