FROM mongo:7

RUN apt-get update && \
    apt-get install -y \
        curl \
        unzip \
        jq \
        coreutils \
    && rm -rf /var/lib/apt/lists/*

RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf awscliv2.zip aws/

RUN mkdir -p /backups
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
WORKDIR /backups
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]