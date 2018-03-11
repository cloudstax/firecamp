FROM telegraf:1.5

RUN mkdir -p /firecamp/

COPY input_redis.conf /firecamp/
COPY output_cloudwatch.conf /firecamp/
COPY telegraf.conf /firecamp/

COPY entrypoint.sh /

CMD ["telegraf", "--config", "/firecamp/telegraf.conf"]
