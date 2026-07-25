FROM golang:1.22-bookworm AS go-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/molstar ./cmd/molstar

FROM node:22-bookworm AS node-deps

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    g++ \
    libcairo2-dev \
    libegl1 \
    libgif-dev \
    libgl1-mesa-dev \
    libglew-dev \
    libglu1-mesa-dev \
    libjpeg-dev \
    libpango1.0-dev \
    librsvg2-dev \
    libxi-dev \
    make \
    pkg-config \
    python3 \
  && rm -rf /var/lib/apt/lists/*

RUN ln -s /usr/bin/python3 /usr/local/bin/python

COPY package.json package-lock.json .npmrc ./
RUN npm ci --omit=dev

FROM node:22-bookworm

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    libcairo2 \
    libegl1 \
    libgif7 \
    libgl1 \
    libglu1-mesa \
    libjpeg62-turbo \
    libpango-1.0-0 \
    libpangocairo-1.0-0 \
    librsvg2-2 \
    libx11-6 \
    libxext6 \
    libxi6 \
    python3 \
    python3-dev \
    xauth \
    xvfb \
  && rm -rf /var/lib/apt/lists/*

COPY --from=node-deps /app/node_modules ./node_modules
COPY scripts ./scripts
COPY schema ./schema
COPY examples ./examples
COPY docs ./docs
COPY python ./python
COPY package.json ./
COPY --from=go-build /out/molstar /usr/local/bin/molstar

ENV LIBGL_ALWAYS_SOFTWARE="1"
ENV LIBGL_DRI3_DISABLE="1"
ENV MOLSTAR_RENDER="xvfb-run -a node /app/scripts/render-mvs.js"

ENTRYPOINT ["molstar"]
CMD ["doctor"]
