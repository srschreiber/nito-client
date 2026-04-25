# Minimal worker image for the nito-bot hello example.
#
# Scripts are bind-mounted at /scripts:ro by the bot at run time, so this
# image only needs to provide whatever runtime the scripts call. For
# `hello.sh` that's just POSIX sh, which alpine ships via busybox.
#
# To extend for a real bot, uncomment the apk line and add whatever your
# scripts need (curl for HTTP APIs, jq for JSON, python3 / node / ruby,
# etc.). The bot re-runs this image each time you `make bot-example-image`.
FROM alpine:3.20

ENV MOTTO="Carpe diem"

# curl + jq are what the example !ask script uses to talk to Gemini.
# Trim or extend this list to match what your scripts call.
RUN apk add --no-cache curl jq
