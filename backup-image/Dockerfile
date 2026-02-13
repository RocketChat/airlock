FROM mongo:7.0 AS awscli

RUN apt-get update && apt-get install curl -y && curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "/tmp/awscliv2.zip"

FROM mongo:7

RUN apt-get update && \
    apt-get install -y \
        curl \
        unzip \
        jq \
        coreutils \
    && rm -rf /var/lib/apt/lists/*
	
COPY --from=awscli /tmp/awscliv2.zip /tmp/awscliv2.zip

RUN cd /tmp && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf awscliv2.zip aws/

RUN mkdir -p /backups
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
WORKDIR /backups
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]