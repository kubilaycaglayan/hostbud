# hostbud-e2e-ctl: node + docker CLI (talks to the host daemon via the
# mounted socket; e2e only, never part of a deployment).
FROM node:24.21.0-alpine
LABEL hostbud.image="1"
RUN apk add --no-cache docker-cli
COPY server.mjs /ctl/server.mjs
CMD ["node", "/ctl/server.mjs"]
