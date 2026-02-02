FROM golang:1 AS util

WORKDIR /app
COPY go.mod ./
COPY go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
	--mount=type=cache,target=/go/pkg/mod \
	CGO_ENABLED=0 GOOS=linux go build -v -o /app/bin/start ./cmd/start

FROM restatedev/restate:1.6

# Ingress
EXPOSE 8080
# Admin
EXPOSE 9070
# Mesh Communication
EXPOSE 5122

VOLUME [ "/restate-data" ]

COPY --from=util /app/bin/start /usr/local/bin/start
# COPY start /usr/local/bin/start
ENTRYPOINT [ "/usr/local/bin/start" ]
