################################
# STEP 1 build executable binary
################################
FROM golang:alpine AS builder

# Install git - required for fetching the dependencies
RUN apk add --update --no-cache ca-certificates git
WORKDIR /app
COPY . .

# Fetch dependencies
RUN go mod download
RUN go mod verify

RUN find .

# Build the binary.
RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/civo-csi


############################
# STEP 2 build a small image
############################
FROM scratch

# Copy our static executable
WORKDIR /app
COPY --from=builder /app/civo-csi /app/civo-csi

# Run the civo-csi binary
ENTRYPOINT ["/app/civo-csi"]
