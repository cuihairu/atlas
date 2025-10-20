FROM node:22

WORKDIR /app

RUN apt-get update && apt-get install -y git && rm -rf /var/lib/apt/lists/*

# Optional: faster CN registry; safe to remove if not needed
RUN npm config set registry https://registry.npmmirror.com

# Pin Quartz to a specific commit for reproducibility (update as needed)
ARG QUARTZ_REF=v4.3.0
RUN git clone https://github.com/jackyzha0/quartz.git . \
    && git checkout "${QUARTZ_REF}" || true \
    && npm i

# Copy our site config/layout to override defaults
COPY quartz.config.ts quartz.layout.ts ./

# Create content/static folders (mounted as volumes in docker-compose)
RUN mkdir -p content static

EXPOSE 3000

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

