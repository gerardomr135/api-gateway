# Lab Dockerfile: builds either binary, selected with --build-arg TARGET=gateway|toysvc.
#
# This is deliberately "good enough for the lab". In M11 you harden it:
# distroless/scratch base, non-root user, -trimpath, -ldflags "-s -w",
# and you explain why each of those matters.

FROM golang:1.24 AS build
WORKDIR /src

# Copy go.mod/go.sum first so dependency downloads are cached in their own
# layer and only re-run when dependencies change, not on every code edit.
COPY go.mod ./
# COPY go.sum ./        # uncomment once you add your first dependency
RUN go mod download

COPY . .
ARG TARGET=toysvc
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/${TARGET}

FROM alpine:3.20
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
