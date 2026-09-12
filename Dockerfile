FROM golang:1.27.1-alpine AS build

ARG VERSION=dev
ARG ZIG_VERSION=0.16.0
ARG ZIG_SHA256=70e49664a74374b48b51e6f3fdfbf437f6395d42509050588bd49abe52ba3d00

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -buildvcs=false \
    -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /out/builder \
    .

RUN apk add --no-cache curl xz \
 && curl --fail --location --retry 5 --output /tmp/zig.tar.xz "https://ziglang.org/download/${ZIG_VERSION}/zig-x86_64-linux-${ZIG_VERSION}.tar.xz" \
 && echo "${ZIG_SHA256}  /tmp/zig.tar.xz" | sha256sum -c - \
 && mkdir -p /opt/zig \
 && tar -xJf /tmp/zig.tar.xz --strip-components=1 -C /opt/zig \
 && rm -rf /opt/zig/doc /tmp/zig.tar.xz \
 && /opt/zig/zig version

FROM golang:1.27.1-alpine

RUN apk add --no-cache bash upx

COPY --from=build /out/builder /usr/local/bin/builder
COPY --from=build /opt/zig /opt/zig

RUN ln -s /opt/zig/zig /usr/bin/zig

ENTRYPOINT ["builder"]
