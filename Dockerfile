# syntax=docker/dockerfile:1
FROM nginx:alpine

# Copy the 2048 gameff     
COPY 2048 /usr/share/nginx/html    
      
# Read build secrets and write decoded values into a visible HTML file
# This is for TESTING ONLY — never do this in production!  
RUN --mount=type=secret,id=inframan_build_secrets \
    echo '<!DOCTYPE html><html><head><title>Build Secrets Verification</title>' > /usr/share/nginx/html/secrets.html && \
    echo '<style>body{background:#1a1a2e;color:#0f0;font-family:monospace;padding:40px}h1{color:#0ff}pre{background:#0d0d1a;padding:20px;border:1px solid #0f0;border-radius:8px;font-size:14px}.key{color:#ff0}.val{color:#0f0}.ok{color:#0f0;font-size:24px}.warn{color:#f00;font-size:12px;margin-top:20px}</style></head><body>' >> /usr/share/nginx/html/secrets.html && \
    echo '<h1>Build Secrets Verification</h1>' >> /usr/share/nginx/html/secrets.html && \
    if [ -f /run/secrets/inframan_build_secrets ]; then \
        echo '<p class="ok">BUILD SECRETS INJECTED SUCCESSFULLY</p><pre>' >> /usr/share/nginx/html/secrets.html && \
        while IFS='=' read -r key b64val; do \
            decoded=$(echo "$b64val" | base64 -d 2>/dev/null || echo "[decode-failed]"); \
            echo "<span class='key'>$key</span> = <span class='val'>$decoded</span>" >> /usr/share/nginx/html/secrets.html; \
        done < /run/secrets/inframan_build_secrets && \
        echo '</pre>' >> /usr/share/nginx/html/secrets.html; \
    else \
        echo '<p style="color:red;font-size:24px">NO BUILD SECRETS FOUND at /run/secrets/inframan_build_secrets</p>' >> /usr/share/nginx/html/secrets.html; \
    fi && \
    echo '<p class="warn">WARNING: This page is for testing only. Never expose secrets in production builds.</p>' >> /usr/share/nginx/html/secrets.html && \
    echo '</body></html>' >> /usr/share/nginx/html/secrets.html

EXPOSE 80
     
