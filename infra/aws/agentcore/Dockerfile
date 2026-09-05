FROM node:22-alpine@sha256:c610fcdfb1d5b4740dd70c284ed3cb16bb857e0f7166196e36a5501df7a3aa32
RUN apk add --no-cache python3 bash git patch \
    && addgroup -g 10001 harness \
    && adduser -D -u 10001 -G harness harness \
    && mkdir /workspace \
    && chown harness:harness /workspace
COPY server.py /opt/harness/server.py
USER 10001:10001
WORKDIR /workspace
ENV PYTHONDONTWRITEBYTECODE=1
EXPOSE 8080
ENTRYPOINT ["python3", "/opt/harness/server.py"]
