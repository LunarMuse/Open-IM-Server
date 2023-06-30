// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package zookeeper

import (
	"context"
	"fmt"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/common/log"
	"google.golang.org/grpc"
	"strings"
)

func newClientConnInterface(cc grpc.ClientConnInterface) grpc.ClientConnInterface {
	return &clientConnInterface{cc: cc}
}

type clientConnInterface struct {
	cc grpc.ClientConnInterface
}

func (c *clientConnInterface) callOptionToString(opts []grpc.CallOption) string {
	arr := make([]string, 0, len(opts)+1)
	arr = append(arr, fmt.Sprintf("opts len: %d", len(opts)))
	for i, opt := range opts {
		arr = append(arr, fmt.Sprintf("[%d:%T]", i, opt))
	}
	return strings.Join(arr, ", ")
}

func (c *clientConnInterface) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	log.ZDebug(ctx, "grpc.ClientConnInterface.Invoke in", "method", method, "args", args, "reply", reply, "opts", c.callOptionToString(opts))
	if err := c.cc.Invoke(ctx, method, args, reply, opts...); err != nil {
		log.ZError(ctx, "grpc.ClientConnInterface.Invoke error", err, "method", method, "args", args, "reply", reply)
		return err
	}
	log.ZDebug(ctx, "grpc.ClientConnInterface.Invoke success", "method", method, "args", args, "reply", reply)
	return nil
}

func (c *clientConnInterface) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	log.ZDebug(ctx, "grpc.ClientConnInterface.NewStream in", "desc", desc, "method", method, "opts", c.callOptionToString(opts))
	cs, err := c.cc.NewStream(ctx, desc, method, opts...)
	if err != nil {
		log.ZError(ctx, "grpc.ClientConnInterface.NewStream error", err, "desc", desc, "method", method, "opts", len(opts))
		return nil, err
	}
	log.ZDebug(ctx, "grpc.ClientConnInterface.NewStream success", "desc", desc, "method", method, "opts", len(opts))
	return cs, nil
}