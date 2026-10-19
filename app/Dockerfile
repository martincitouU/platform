# ---------- Etapa 1: compilación ----------
FROM golang:1.26 AS build
WORKDIR /src

# Primero solo los archivos de dependencias (ver "caché" abajo)
COPY go.mod go.sum ./
RUN go mod download

# Después el resto del código
COPY . .

# Qué binario compilar: api o worker
ARG CMD=api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${CMD}

# ---------- Etapa 2: imagen final ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]