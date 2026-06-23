# syntax=docker/dockerfile:1
FROM nginx:alpine

# Copy the 2048 game
COPY 2048 /usr/share/nginx/html

# Connectivity test page — calls API_URL to verify service connections
COPY connectivity.html /usr/share/nginx/html/connectivity.html

# nginx:alpine runs envsubst on /etc/nginx/templates/*.template at startup,
# writing the rendered file to /etc/nginx/conf.d/. This injects ${API_URL}
# (set by Inframan from the `api` connection) into the /api/ proxy block.
RUN rm /etc/nginx/conf.d/default.conf
COPY nginx.conf.template /etc/nginx/templates/default.conf.template

EXPOSE 80

