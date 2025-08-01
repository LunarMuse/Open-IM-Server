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

package msgtransfer

import (
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/mq"

	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	pbmsg "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/tools/log"
	"google.golang.org/protobuf/proto"
)

type OnlineHistoryMongoConsumerHandler struct {
	msgTransferDatabase controller.MsgTransferDatabase
	config              *Config
	webhookClient       *webhook.Client
}

func NewOnlineHistoryMongoConsumerHandler(database controller.MsgTransferDatabase, config *Config) *OnlineHistoryMongoConsumerHandler {
	return &OnlineHistoryMongoConsumerHandler{
		msgTransferDatabase: database,
		config:              config,
		webhookClient:       webhook.NewWebhookClient(config.WebhooksConfig.URL),
	}
}

func (mc *OnlineHistoryMongoConsumerHandler) HandleChatWs2Mongo(val mq.Message) {
	ctx := val.Context()
	key := val.Key()
	msg := val.Value()
	msgFromMQ := pbmsg.MsgDataToMongoByMQ{}
	err := proto.Unmarshal(msg, &msgFromMQ)
	if err != nil {
		log.ZError(ctx, "unmarshall failed", err, "key", key, "len", len(msg))
		return
	}
	if len(msgFromMQ.MsgData) == 0 {
		log.ZError(ctx, "msgFromMQ.MsgData is empty", nil, "key", key, "msg", msg)
		return
	}
	log.ZDebug(ctx, "mongo consumer recv msg", "msgs", msgFromMQ.String())
	err = mc.msgTransferDatabase.BatchInsertChat2DB(ctx, msgFromMQ.ConversationID, msgFromMQ.MsgData, msgFromMQ.LastSeq)
	if err != nil {
		log.ZError(ctx, "single data insert to mongo err", err, "msg", msgFromMQ.MsgData, "conversationID", msgFromMQ.ConversationID)
		prommetrics.MsgInsertMongoFailedCounter.Inc()
	} else {
		prommetrics.MsgInsertMongoSuccessCounter.Inc()
		val.Mark()
	}

	for _, msgData := range msgFromMQ.MsgData {
		switch msgData.SessionType {
		case constant.SingleChatType:
			mc.webhookAfterSendSingleMsg(ctx, &mc.config.WebhooksConfig.AfterSendSingleMsg, msgData)
		case constant.ReadGroupChatType:
			mc.webhookAfterSendGroupMsg(ctx, &mc.config.WebhooksConfig.AfterSendGroupMsg, msgData)
		}
	}

	//var seqs []int64
	//for _, msg := range msgFromMQ.MsgData {
	//	seqs = append(seqs, msg.Seq)
	//}
	//if err := mc.msgTransferDatabase.DeleteMessagesFromCache(ctx, msgFromMQ.ConversationID, seqs); err != nil {
	//	log.ZError(ctx, "remove cache msg from redis err", err, "msg",
	//		msgFromMQ.MsgData, "conversationID", msgFromMQ.ConversationID)
	//}
}
