# Use the AWS Lambda provided runtime base image (Amazon Linux 2023)
FROM public.ecr.aws/lambda/provided:al2023

# Copy the pre-built binary
COPY pim ${LAMBDA_TASK_ROOT}

# Set the entrypoint to run the binary
ENTRYPOINT ["./pim"]
