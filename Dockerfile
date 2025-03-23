FROM golang:latest

WORKDIR /app

COPY . .

RUN go build -o template-wizard .

EXPOSE 8080

CMD ["./template-wizard"]