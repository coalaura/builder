ARG GO_VERSION=1.27.1

FROM golang:${GO_VERSION}-alpine AS build

ARG GO_VERSION
ARG VERSION=dev
ARG ZIG_VERSION=0.17.0
ARG ZIG_SHA256=1cbe9df9f27e6b78d14ccbca43b6703a404ef79ef1c463de901d7f088d4e2026

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

RUN apk add --no-cache ca-certificates curl xz \
 && curl --fail --location --retry 5 --output /tmp/zig.tar.xz "https://ziglang.org/download/${ZIG_VERSION}/zig-x86_64-linux-${ZIG_VERSION}.tar.xz" \
 && echo "${ZIG_SHA256}  /tmp/zig.tar.xz" | sha256sum -c - \
 && mkdir -p /opt/zig \
 && tar -xJf /tmp/zig.tar.xz --strip-components=1 -C /opt/zig \
 && rm -rf /opt/zig/doc /tmp/zig.tar.xz \
 && /opt/zig/zig version

RUN curl --fail --location --retry 5 --output /tmp/pace.tar.gz "https://github.com/coalaura/pace/releases/download/pace${GO_VERSION}/pace-${GO_VERSION}-linux-amd64.tar.gz" \
 && mkdir -p /out/pace \
 && tar -xzf /tmp/pace.tar.gz --strip-components=1 -C /out/pace bin/pace bin/compilepe bin/asmpe \
 && rm /tmp/pace.tar.gz

FROM golang:${GO_VERSION}-alpine AS runtime

RUN apk add --no-cache \
    bash \
    ca-certificates \
    git \
    upx

COPY --from=build /out/builder /usr/local/bin/builder
COPY --from=build /opt/zig /opt/zig
COPY --from=build /out/pace/ /usr/local/go/bin/

RUN ln -s /opt/zig/zig /usr/bin/zig \
 && go version \
 && pace version \
 && compilepe -V \
 && asmpe -V

ENTRYPOINT ["builder"]

FROM runtime AS macos

COPY --from=macos-sdk / /opt/osxcross/SDK/MacOSX.sdk/

FROM runtime AS standard
