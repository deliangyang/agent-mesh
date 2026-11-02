# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/internal/relay/web
COPY internal/relay/web/package.json internal/relay/web/package-lock.json ./
RUN npm ci
COPY internal/relay/web/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/relay/web/dist internal/relay/web/dist
RUN mkdir -p internal/relay/web/dist && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/relay ./cmd/relay \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/node ./cmd/node \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ctl ./cmd/ctl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/relay /relay
COPY --from=build /out/node /node
COPY --from=build /out/ctl /ctl
EXPOSE 8443 7700
USER nonroot
ENTRYPOINT ["/relay"]
CMD ["-listen", "0.0.0.0:8443", "-web"]
