# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM node:25-alpine@sha256:bdf2cca6fe3dabd014ea60163eca3f0f7015fbd5c7ee1b0e9ccb4ced6eb02ef4 AS frontend-build
WORKDIR /src/web/frontend
COPY web/frontend/package.json web/frontend/package-lock.json ./
RUN npm ci
COPY web/frontend ./
RUN npm run build

FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend-build /src/internal/web/dist ./internal/web/dist
ARG LAKE_PASS_VERSION=dev
ARG LAKE_PASS_REVISION=""
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/jaysqvl/lake-pass-bot/internal/buildinfo.Version=${LAKE_PASS_VERSION} -X github.com/jaysqvl/lake-pass-bot/internal/buildinfo.Revision=${LAKE_PASS_REVISION}" \
    -o /out/lake-pass-bot ./cmd/lake-pass-bot

FROM mcr.microsoft.com/playwright/python:v1.63.0-noble@sha256:72bd171a9ffc2b4b59532aaa6210e21014d07093120dc25528870c0b840da1f0

LABEL net.unraid.docker.icon="https://raw.githubusercontent.com/jaysqvl/lake-pass-bot/main/deploy/lake-pass-bot.png" \
    net.unraid.docker.webui="http://[IP]:[PORT:8080]/"

ENV APPDATA_DIR=/appdata \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /app
COPY actions/requirements.lock /tmp/lake-pass-actions-requirements.txt
# Keep the pinned browser image; apply Noble's ABI-compatible OpenSSL fixes.
# Refuse an unexpected resolver change rather than upgrading unrelated packages.
RUN apt-get update \
    && LC_ALL=C apt-get --simulate --no-install-recommends --only-upgrade install libssl3t64 openssl > /tmp/lake-pass-openssl-plan \
    && cat /tmp/lake-pass-openssl-plan \
    && awk '/^(Remv|Purg) / { exit 1 } /^Inst / { if ($2 != "libssl3t64" && $2 != "openssl") exit 1 }' /tmp/lake-pass-openssl-plan \
    && apt-get --yes --no-install-recommends --only-upgrade install libssl3t64 openssl \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libssl3t64)" ge 3.0.13-0ubuntu3.16 \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' openssl)" ge 3.0.13-0ubuntu3.16 \
    && LC_ALL=C apt-get --simulate purge gstreamer1.0-plugins-bad libgstreamer-plugins-bad1.0-0 > /tmp/lake-pass-purge-plan \
    && cat /tmp/lake-pass-purge-plan \
    && awk '/^Inst / { exit 1 } /^(Remv|Purg) / { if ($2 != "gstreamer1.0-plugins-bad" && $2 != "libgstreamer-plugins-bad1.0-0") exit 1 }' /tmp/lake-pass-purge-plan \
    && apt-get --yes --no-auto-remove purge gstreamer1.0-plugins-bad libgstreamer-plugins-bad1.0-0 \
    && python -m pip install --no-cache-dir --only-binary=:all: --require-hashes --requirement /tmp/lake-pass-actions-requirements.txt \
    && python -m pip check \
    && python -m pip uninstall --yes virtualenv msgpack setuptools \
    && python -m pip uninstall --yes pip \
    && rm /tmp/lake-pass-actions-requirements.txt /tmp/lake-pass-purge-plan /tmp/lake-pass-openssl-plan \
    && rm -rf /root/.cache /home/pwuser/.cache /var/lib/apt/lists/* \
    && mkdir -p /appdata \
    && chown -R pwuser:pwuser /app /appdata
COPY actions/src/lake_pass_actions /usr/local/lib/python3.12/dist-packages/lake_pass_actions
COPY actions/src/buntzen_actions /usr/local/lib/python3.12/dist-packages/buntzen_actions
COPY --from=go-build /out/lake-pass-bot /usr/local/bin/lake-pass-bot
# Preserve existing operator commands during the rename.
RUN ln -s lake-pass-bot /usr/local/bin/buntzen

USER pwuser
EXPOSE 8080
VOLUME ["/appdata"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/healthz', timeout=3).read()"]

ENTRYPOINT ["/usr/local/bin/lake-pass-bot"]
CMD ["serve"]
