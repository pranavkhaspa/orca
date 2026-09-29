# ORCA API image.
#
# The service has no third-party Go dependencies, so the build stage needs
# nothing but the toolchain and the final image needs nothing but a static
# binary. That is what keeps the runtime image at a few tens of megabytes and
# leaves no dependency to be stale when the demo is running.

# ---- build ------------------------------------------------------------------
FROM golang:1.24-alpine AS build

WORKDIR /src

# go.mod lists no requirements, but copying it first keeps this layer cached
# when only source files change.
COPY go.mod ./
COPY . .

# CGO is disabled so the binary runs on a distroless base. -trimpath keeps build
# paths out of the binary so the image is reproducible.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/orca ./cmd/orca

# ---- runtime ----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# Only the binary is copied; the coastal table and the offline snapshot are
# embedded in it, so there is no data directory to mount and no filesystem layout
# that can break in deployment.
COPY --from=build /out/orca /orca

# distroless nonroot runs as uid 65532.
USER nonroot:nonroot
EXPOSE 8080

# The service answers on PORT, which Render sets. The default keeps a bare
# `docker run` working without any flags.
ENV PORT=8080

ENTRYPOINT ["/orca"]
