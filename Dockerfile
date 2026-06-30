ARG GO_VERSION=1.25.4

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 \
	GOOS=${TARGETOS:-linux} \
	GOARCH=${TARGETARCH:-amd64} \
	go build \
		-tags=netgo,osusergo,nomsgpack \
		-trimpath \
		-ldflags="-s -w -buildid=" \
		-o /out/web2fa .

FROM scratch AS runtime

COPY --from=builder /out/web2fa /web2fa

USER 65532:65532
EXPOSE 8081

ENTRYPOINT ["/web2fa"]
