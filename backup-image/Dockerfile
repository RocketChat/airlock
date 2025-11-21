FROM mongo:7

# Install AWS CLI, jq, and other utilities
RUN apt-get update && \
    apt-get install -y \
        curl \
        unzip \
        jq \
        coreutils \
    && rm -rf /var/lib/apt/lists/*

# Install AWS CLI v2
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf awscliv2.zip aws/

# Create backup directory
RUN mkdir -p /backups

# Copy entrypoint script
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Set working directory
WORKDIR /backups

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]