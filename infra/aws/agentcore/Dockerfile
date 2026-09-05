FROM node:22-alpine@sha256:c610fcdfb1d5b4740dd70c284ed3cb16bb857e0f7166196e36a5501df7a3aa32 AS guard-build
RUN apk add --no-cache build-base linux-headers libseccomp-dev
COPY isolation/guard.c /src/guard.c
RUN cc -O2 -Wall -Wextra -Werror -o /harness-guard /src/guard.c -lseccomp

FROM node:22-alpine@sha256:c610fcdfb1d5b4740dd70c284ed3cb16bb857e0f7166196e36a5501df7a3aa32
RUN apk add --no-cache python3 bash git patch libseccomp \
    && addgroup -g 10001 harness \
    && adduser -D -u 10001 -G harness harness \
    && mkdir -p /harness-root/repo \
    && chown -R harness:harness /harness-root \
    && ln -s /harness-root/repo /workspace
COPY --from=guard-build /harness-guard /usr/local/bin/harness-guard
COPY harness-service /opt/harness/service
COPY isolation/checks.py /opt/harness/isolation-checks.py
USER 10001:10001
WORKDIR /workspace
ENV PYTHONDONTWRITEBYTECODE=1
EXPOSE 8080
ENTRYPOINT ["/opt/harness/service"]
