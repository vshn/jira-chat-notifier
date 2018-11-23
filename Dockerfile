#####################
# STEP 1 build binary
#####################
FROM golang:alpine as builder

# Prepare needed packages for building
RUN apk update && apk add --no-cache git ca-certificates && \
    adduser -D -g '' appuser

# Workdir must be outside of GOPATH because of go mod usage
WORKDIR /src/jcn

# Download modules for leveraging docker build cache
COPY go.mod go.sum ./
RUN go mod download

# Add code and build app
COPY . .
RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/jcn

############################
# STEP 2 build runtime image
############################
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /go/bin/jcn /go/bin/jcn

USER appuser
EXPOSE 8081

ENTRYPOINT ["/go/bin/jcn"]