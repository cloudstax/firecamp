FROM debian:jessie-slim

RUN apt-get update && apt-get install -y ca-certificates

# TODO send logs to AWS CloudWatch
RUN mkdir -p /var/log/openmanage

COPY openmanage-manageserver /
COPY docker-entrypoint.sh /

ENTRYPOINT ["/docker-entrypoint.sh"]

EXPOSE 27040
