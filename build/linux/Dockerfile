# Builds the Linux binary without a Linux machine:
#   make build-linux              # amd64 (emulated on Apple silicon)
#   make build-linux ARCH=arm64
# The binary lands in build/bin/linux-<arch>/CommitTree. It links against
# glibc 2.36 and webkit2gtk-4.1 (Debian 12, Ubuntu 22.04+, Fedora 36+).
FROM golang:1.26-bookworm AS build

RUN apt-get update \
 && apt-get install -y --no-install-recommends libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config \
 && rm -rf /var/lib/apt/lists/*

COPY --from=node:22-bookworm /usr/local/bin/node /usr/local/bin/node
COPY --from=node:22-bookworm /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

RUN go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY frontend/package.json frontend/package-lock.json frontend/
RUN cd frontend && npm ci
COPY . .
RUN wails build -tags webkit2_41

FROM scratch
COPY --from=build /src/build/bin/CommitTree /CommitTree
