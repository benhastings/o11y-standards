# Weaver gate image: Weaver plus the org registry, policies, policy data and a
# baked-in copy of OpenTelemetry semconv. CI jobs run it as a service (a
# sidecar container in the job pod on the Kubernetes executor), so nothing is
# downloaded at job time.
#
# Build from the repo root:
#   docker build -f ci/gate-image/Dockerfile -t weaver-gate .
#
# Point WEAVER_IMAGE at your registry mirror if runners can't pull Docker Hub.
ARG WEAVER_IMAGE=otel/weaver:v0.27.0@sha256:3049b4079049d4abb1b5632f511ada2c33505a1c60f3f8535e93f87f0696f056

FROM ${WEAVER_IMAGE} AS semconv
ARG SEMCONV_VERSION=1.43.0
ARG SEMCONV_URL=https://github.com/open-telemetry/semantic-conventions/archive/refs/tags/v${SEMCONV_VERSION}.tar.gz
# sha256 of the v1.43.0 archive. Clear it (--build-arg SEMCONV_SHA256=) only
# when moving to a new version, then pin the new value.
ARG SEMCONV_SHA256=ec7886ff60f500814a6f72c9f4955fcd040c019745e6beccb0e61886feae8618
USER root
RUN wget -qO /tmp/semconv.tgz "$SEMCONV_URL" \
 && if [ -n "$SEMCONV_SHA256" ]; then echo "$SEMCONV_SHA256  /tmp/semconv.tgz" | sha256sum -c -; fi \
 && mkdir -p /opt/semconv \
 && tar -xzf /tmp/semconv.tgz -C /opt/semconv --strip-components=1 \
 # semconv 1.43's manifest predates file_format; `registry check --future`
 # rejects it without this line.
 && { grep -q '^file_format:' /opt/semconv/model/manifest.yaml \
      || sed -i '1i file_format: definition_manifest/2.0' /opt/semconv/model/manifest.yaml; }

FROM ${WEAVER_IMAGE}
USER root
COPY --from=semconv /opt/semconv/model /opt/semconv/model
COPY weaver/ /opt/kfo11y/weaver/
COPY ci/gate-image/bin/ /opt/kfo11y/bin/
# Resolve semconv from the baked-in copy instead of GitHub.
RUN sed -i 's#^\(\s*registry_path:\).*#\1 /opt/semconv/model#' /opt/kfo11y/weaver/registry/manifest.yaml \
 && chmod 0755 /opt/kfo11y/bin/*
USER weaver
ENV PATH="/opt/kfo11y/bin:/weaver:${PATH}"
EXPOSE 4317 4320
ENTRYPOINT ["/opt/kfo11y/bin/gate-serve"]
