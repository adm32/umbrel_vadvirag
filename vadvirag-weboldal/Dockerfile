# Vadvirág weboldal – Umbrel / Docker kép.
# Az előre lefordított, statikus programot másolja be (a weboldal bele van fordítva),
# így az indításhoz nem kell internet.
FROM scratch
ARG TARGETARCH=amd64
COPY bin/weboldal-linux-${TARGETARCH} /weboldal
ENV PORT=8080 TZ=Europe/Budapest
EXPOSE 8080
ENTRYPOINT ["/weboldal"]
