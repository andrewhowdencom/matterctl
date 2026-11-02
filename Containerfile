FROM docker.io/library/golang:1.26.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/matterctl ./cmd/matterctl

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/matterctl /matterctl
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/matterctl"]
CMD ["serve"]
