#
# Copyright 2022 OpsMx, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License")
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

#
# Install the latest versions of our mods.  This is done as a separate step
# so it will pull from an image cache if possible, unless there are changes.
#
FROM golang:1.17-alpine AS buildmod
ENV CGO_ENABLED=0
RUN mkdir /build
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download

#
# Compile the code.
#
FROM buildmod AS build-binaries
COPY . .

RUN mkdir /out
RUN go build -ldflags="-s -w" -o /out/go-demo-web app/go-demo-web/*.go

FROM scratch AS base-image

#
# Build the go-demo-web image.  This should be a --target on docker build.
#
FROM base-image AS go-demo-web-image
WORKDIR /app
COPY --from=build-binaries /out/go-demo-web /app
ARG GIT_BRANCH
ENV GIT_BRANCH=${GIT_BRANCH}
ARG GIT_HASH
ENV GIT_HASH=${GIT_HASH}
EXPOSE 8090
CMD ["/app/go-demo-web"]
