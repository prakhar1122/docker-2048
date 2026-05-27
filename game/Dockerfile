# syntax=docker/dockerfile:1
FROM nginx:alpine

# Copy the 2048 game
COPY 2048 /usr/share/nginx/html

# Connectivity test page — calls API_URL to verify service connections
COPY connectivity.html /usr/share/nginx/html/connectivity.html

EXPOSE 80

