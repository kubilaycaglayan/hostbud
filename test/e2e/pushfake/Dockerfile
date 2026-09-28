# hostbud-e2e-pushfake: a fake Web Push service (V2-M3; e2e only, never part
# of a deployment). The e2e app's HOSTBUD_PUSH_TEST_ENDPOINT points here.
FROM node:24.21.0-alpine
LABEL hostbud.image="1"
COPY server.mjs /pushfake/server.mjs
USER node
CMD ["node", "/pushfake/server.mjs"]
