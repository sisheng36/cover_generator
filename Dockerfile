FROM python:3.11-slim

ENV TZ=Asia/Shanghai
ENV PYTHONUNBUFFERED=1
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

RUN mkdir -p /data

WORKDIR /app

# 构建期版本号：由 CI 传入（tag → X.Y.Z；main → "Development version"）。
# 未传入时写一个占位值，保证 app/version.py 总能读到文件。
ARG VERSION=Development version
RUN mkdir -p app && printf '%s' "${VERSION}" > app/VERSION

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8055

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8055", "--log-level", "info"]
