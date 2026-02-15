FROM alpine:3.23

RUN apk update && \
    apk --no-cache add age ca-certificates libc6-compat curl bash mongodb-tools jq unzip uutils-coreutils aws-cli jo

RUN mkdir -p /backups

COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /backups

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]