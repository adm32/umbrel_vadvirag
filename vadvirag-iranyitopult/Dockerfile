# Vadvirág Irányítópult – Umbrel / Docker kép.
# Az előre lefordított, statikus programot másolja be (a felület bele van fordítva),
# így az indításhoz nem kell internet.
FROM scratch
ARG TARGETARCH=amd64
COPY bin/iranyitopult-linux-${TARGETARCH} /iranyitopult
ENV DATA_DIR=/data PORT=8080 TZ=Europe/Budapest
EXPOSE 8080
ENTRYPOINT ["/iranyitopult"]
