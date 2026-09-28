FROM node:24.21.0-alpine
LABEL hostbud.image="1"
COPY server.mjs /llmfake/server.mjs
USER node
CMD ["node", "/llmfake/server.mjs"]
