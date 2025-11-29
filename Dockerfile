# Use the official Nginx image (lightweight & pre-configured)
FROM nginx:alpine

# Copy the CONTENTS of the '2048' folder to the Nginx web root
COPY 2048 /usr/share/nginx/html

# Expose port 80
EXPOSE 80
