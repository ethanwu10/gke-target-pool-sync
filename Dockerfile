FROM golang:1.14 as builder

WORKDIR /go/src/gke-target-pool-sync
COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 go build -v

FROM scratch as run

COPY --from=builder /go/src/gke-target-pool-sync/gke-target-pool-sync .

CMD ["/gke-target-pool-sync"]
