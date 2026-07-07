# syntax=docker/dockerfile:1
# check=skip=FromPlatformFlagConstDisallowed
#
# Build & verify the NATIVE ActiveSpaces VectorDB connector.
#
# The native connector links the TIBCO ActiveSpaces client (tibdg) directly via
# CGO, so it can only be built on linux/amd64 with the AS SDK present. This image
# uses the AS SDK development image as the build base and sources the proprietary
# tibdg client from it at build time — nothing TIBCO-licensed is ever copied from
# (or committed to) this repository.
#
# Build:
#   docker build -f Dockerfile -t as-native-connector:build .
# The build fails if the native connector does not compile against the AS SDK.
#
# NOTE: building the full Flogo *application* that uses this connector is a
# separate step (the Flogo toolchain generates the engine) — see README.md
# ("Building a native Flogo app").

ARG AS_IMAGE=<>/as-dev-ftlserver:5.2.0
FROM --platform=linux/amd64 ${AS_IMAGE}

# The AS SDK base image (Ubuntu 24.04) ships without a Go toolchain or a C
# compiler; install both — CGO needs gcc to link the native tibdg client.
ARG GO_VERSION=1.24.1
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz

# TIBCO ActiveSpaces / FTL SDK locations inside the base image.
ENV TIBDG_ROOT=/opt/tibco/as/current-version \
    FTL_LIB=/opt/tibco/ftl/7.2/lib

# CGO must be on to link the native client.
ENV PATH=/usr/local/go/bin:${PATH} \
    CGO_ENABLED=1 \
    GOTOOLCHAIN=auto \
    GOFLAGS=-mod=mod \
    GOCACHE=/tmp/gocache \
    CGO_CFLAGS="-I/opt/tibco/as/current-version/include -I/opt/tibco/as/current-version/ftl/include" \
    CGO_LDFLAGS="-L/opt/tibco/as/current-version/lib -L/opt/tibco/ftl/7.2/lib -Wl,-rpath,/opt/tibco/as/current-version/lib,-rpath,/opt/tibco/ftl/7.2/lib" \
    LD_LIBRARY_PATH=/opt/tibco/as/current-version/lib:/opt/tibco/ftl/7.2/lib

WORKDIR /src

# Connector source (the proprietary dep/tibco.com/tibdg is git-ignored and absent).
COPY . /src

# Provide tibdg from the AS SDK in the base image (go.mod: tibco.com/tibdg => ./dep/tibco.com/tibdg).
RUN mkdir -p dep/tibco.com \
    && cp -R "${TIBDG_ROOT}/samples/golang/src/tibco.com/tibdg" dep/tibco.com/tibdg

# Compile every package — this is the real gate: it builds the CGO native client
# (native_client.go) against the AS SDK.
RUN go build ./... \
    && echo "OK: native ActiveSpaces connector compiled against the AS SDK"

CMD ["/bin/bash"]
