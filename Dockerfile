# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /

# Copy the binary the GH Action built on an earlier step
COPY bin/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
