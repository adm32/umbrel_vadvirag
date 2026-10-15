# Vadvirág rendeléskezelő – Umbrel / Docker kép.
# Az előre lefordított, statikus programot másolja be, így az indításhoz
# nem kell internet (nincs letöltendő alapkép).
FROM scratch
ARG TARGETARCH=amd64
COPY bin/rendeleskezelo-linux-${TARGETARCH} /rendeleskezelo
ENV DATA_DIR=/data PORT=8080 TZ=Europe/Budapest
EXPOSE 8080
ENTRYPOINT ["/rendeleskezelo", "-nobrowser"]
