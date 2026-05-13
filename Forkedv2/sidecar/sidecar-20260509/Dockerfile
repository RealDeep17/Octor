FROM python:3.10-slim
WORKDIR /app
RUN pip install fastapi uvicorn requests python-multipart
COPY main.py .
CMD ["python", "main.py"]