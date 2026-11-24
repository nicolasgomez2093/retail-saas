FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /retail ./cmd/retail

FROM debian:bookworm-slim
COPY --from=build /retail /usr/local/bin/retail
USER 65534:65534
ENTRYPOINT ["retail"]
CMD ["serve"]
