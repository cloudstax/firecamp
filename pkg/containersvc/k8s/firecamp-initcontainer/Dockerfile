#FROM debian:jessie-slim
FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y ca-certificates

COPY firecamp-initcontainer /
COPY docker-entrypoint.sh /

ENTRYPOINT ["/docker-entrypoint.sh"]
