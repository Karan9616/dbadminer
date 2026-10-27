FROM golang:1.26 AS build

WORKDIR /src
COPY . .
ARG VERSION=0.0.0-dev
RUN version="${VERSION#v}" && CGO_ENABLED=0 go build -C cmd/dbadminer -trimpath \
	-ldflags "-s -w -X github.com/dbadminer/dbadminer.Version=${version}" \
	-o /out/dbadminer .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dbadminer /dbadminer
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/dbadminer", "--addr", "0.0.0.0:8080"]
