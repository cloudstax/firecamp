FROM debian:jessie-slim

RUN apt-get update && apt-get install -y ca-certificates

RUN mkdir -p /var/log/openmanage

COPY openmanage-aws-controldb /
COPY docker-entrypoint.sh /

ENTRYPOINT ["/docker-entrypoint.sh"]

EXPOSE 27030
