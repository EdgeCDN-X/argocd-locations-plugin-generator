# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o argocd-locations-plugin-generator main.go

FROM scratch
COPY --from=builder /app/argocd-locations-plugin-generator /argocd-locations-plugin-generator
EXPOSE 8080
ENTRYPOINT ["/argocd-locations-plugin-generator"]