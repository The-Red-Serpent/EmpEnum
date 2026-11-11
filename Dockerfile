FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /empenum .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /empenum /empenum
EXPOSE 8080
ENTRYPOINT ["/empenum"]
CMD ["web", "-addr", "0.0.0.0:8080"]
