FROM golang:1.21-alpine AS builder

WORKDIR /app
RUN apk add git
ENV PRIVATE_REPO_ACCESS_KEY=
ENV GOPRIVATE=
RUN \
    git config \
    --global \
    url."https://rocketchat-cloudbot:$PRIVATE_REPO_ACCESS_KEY@github.com".insteadOf "https://github.com"

COPY go.mod go.sum ./
RUN go mod tidy
COPY . .

RUN \
    CGO_ENABLED=0 \
    GOOS=$(go version | rev | awk '{print $1}' | rev | cut -d/ -f1) \
    GOARCH=$(go version | rev | awk '{print $1}' | rev | cut -d/ -f2) \
    go build -o /app/manager main.go
# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /

# Copy the binary the GH Action built on an earlier step
COPY --from=builder /app/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
