# Cloud Run image for listmonk, built from this repository's source
# (docs/docs/content/cloud-run.md). The service runs the default command and
# listens on $PORT. The schema install and upgrade are not run on start: run the
# same image as a Cloud Run job with the command /listmonk/migrate.sh before
# each deploy.
#
#   docker build -f deploy/cloudrun/Dockerfile --build-arg LISTMONK_VERSION=v6.2.0 -t listmonk-cloudrun .

FROM golang:1.26 AS go

# Build the frontend and the binary, and stuff the static assets into it (make dist).
FROM node:22-bookworm AS build
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH \
    CGO_ENABLED=0
ARG LISTMONK_VERSION=v0.0.0
ENV LISTMONK_VERSION=${LISTMONK_VERSION}
WORKDIR /src
COPY . .
RUN make dist

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -H -u 10001 listmonk
WORKDIR /listmonk
COPY --from=build /src/listmonk .
COPY --chmod=755 deploy/cloudrun/migrate.sh .
USER listmonk

# Cloud Run sets $PORT; 8080 is its default.
ENV PORT=8080
EXPOSE 8080
CMD ["sh", "-c", "export LISTMONK_app__address=0.0.0.0:${PORT} && exec ./listmonk --config ''"]
