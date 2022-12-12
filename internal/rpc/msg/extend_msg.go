package msg

import (
	"Open_IM/pkg/common/db"
	"Open_IM/pkg/common/log"
	"Open_IM/pkg/proto/msg"
	"Open_IM/pkg/proto/sdk_ws"
	"Open_IM/pkg/utils"
	"context"
	go_redis "github.com/go-redis/redis/v8"

	"time"
)

func (rpc *rpcChat) SetMessageReactionExtensions(ctx context.Context, req *msg.SetMessageReactionExtensionsReq) (resp *msg.SetMessageReactionExtensionsResp, err error) {
	log.Debug(req.OperationID, utils.GetSelfFuncName(), "rpc args is:", req.String())
	var rResp msg.SetMessageReactionExtensionsResp
	rResp.ClientMsgID = req.ClientMsgID
	isExists, err := db.DB.JudgeMessageReactionEXISTS(req.ClientMsgID, req.SessionType)
	if err != nil {
		rResp.ErrCode = 100
		rResp.ErrMsg = err.Error()
		for _, value := range req.ReactionExtensionList {
			temp := new(msg.KeyValueResp)
			temp.KeyValue = value
			temp.ErrMsg = err.Error()
			temp.ErrCode = 100
			rResp.Result = append(rResp.Result, temp)
		}
		return &rResp, nil
	}

	if !isExists {
		if !req.IsReact {
			log.Debug(req.OperationID, "redis handle firstly", req.String())
			rResp.MsgFirstModifyTime = utils.GetCurrentTimestampByMill()
			//redis处理
			for k, v := range req.ReactionExtensionList {
				//抢占分布式锁
				err := lockMessageTypeKey(req.ClientMsgID, k)
				if err != nil {
					setKeyResultInfo(&rResp, 100, err.Error(), req.ClientMsgID, k, v)
					continue
				}
				v.LatestUpdateTime = utils.GetCurrentTimestampByMill()
				newerr := db.DB.SetMessageTypeKeyValue(req.ClientMsgID, req.SessionType, k, utils.StructToJsonString(v))
				if newerr != nil {
					setKeyResultInfo(&rResp, 201, newerr.Error(), req.ClientMsgID, k, v)
					continue
				}
				setKeyResultInfo(&rResp, 0, "", req.ClientMsgID, k, v)
			}
			_, err := db.DB.SetMessageReactionExpire(req.ClientMsgID, req.SessionType, time.Duration(24*3)*time.Hour)
			if err != nil {
				log.Error(req.OperationID, "SetMessageReactionExpire err:", err.Error(), req.String())
			}
		} else {
			//mongo处理
		}

	} else {
		log.Debug(req.OperationID, "redis handle secondly", req.String())

		for k, v := range req.ReactionExtensionList {
			//抢占分布式锁
			err := lockMessageTypeKey(req.ClientMsgID, k)
			if err != nil {
				setKeyResultInfo(&rResp, 100, err.Error(), req.ClientMsgID, k, v)
				continue
			}
			redisValue, err := db.DB.GetMessageTypeKeyValue(req.ClientMsgID, req.SessionType, k)
			if err != nil && err != go_redis.Nil {
				setKeyResultInfo(&rResp, 200, err.Error(), req.ClientMsgID, k, v)
				continue
			}
			temp := new(server_api_params.KeyValue)
			utils.JsonStringToStruct(redisValue, temp)
			if v.LatestUpdateTime != temp.LatestUpdateTime {
				setKeyResultInfo(&rResp, 300, "message have update", req.ClientMsgID, k, temp)
				continue
			} else {
				v.LatestUpdateTime = utils.GetCurrentTimestampByMill()
				newerr := db.DB.SetMessageTypeKeyValue(req.ClientMsgID, req.SessionType, k, utils.StructToJsonString(v))
				if newerr != nil {
					setKeyResultInfo(&rResp, 201, newerr.Error(), req.ClientMsgID, k, temp)
					continue
				}
				setKeyResultInfo(&rResp, 0, "", req.ClientMsgID, k, v)
			}

		}
	}
	if !isExists && !req.IsReact {
		ExtendMessageUpdatedNotification(req.OperationID, req.OpUserID, req.SourceID, req.SessionType, req, &rResp, true)
	} else {
		ExtendMessageUpdatedNotification(req.OperationID, req.OpUserID, req.SourceID, req.SessionType, req, &rResp, false)

	}
	log.Debug(req.OperationID, utils.GetSelfFuncName(), "rpc return is:", rResp.String())
	return &rResp, nil

}
func setKeyResultInfo(r *msg.SetMessageReactionExtensionsResp, errCode int32, errMsg, clientMsgID, typeKey string, keyValue *server_api_params.KeyValue) {
	temp := new(msg.KeyValueResp)
	temp.KeyValue = keyValue
	temp.ErrCode = errCode
	temp.ErrMsg = errMsg
	r.Result = append(r.Result, temp)
	_ = db.DB.UnLockMessageTypeKey(clientMsgID, typeKey)
}

func (rpc *rpcChat) GetMessageListReactionExtensions(ctx context.Context, req *msg.OperateMessageListReactionExtensionsReq) (resp *msg.OperateMessageListReactionExtensionsResp, err error) {
	//for _, messageValue := range req.MessageReactionKeyList {
	//	isExists, err := db.DB.JudgeMessageReactionEXISTS(messageValue.ClientMsgID,req.SessionType)
	//	if err != nil {
	//
	//	}
	//	var failedList []*msg.ExtendMsgResp
	//	var successList []*msg.ExtendMsgResp
	//	var oneExtendMsg   msg.ExtendMsg
	//	oneExtendMsg.ClientMsgID = req.ClientMsgID
	//	oneFailedReactionExtensionList:=make(map[string]*msg.KeyValueResp)
	//	oneSuccessReactionExtensionList:=make(map[string]*msg.KeyValueResp)
	//	if !isExists {
	//		if  !req.IsReact {
	//			oneExtendMsg.MsgFirstModifyTime = utils.GetCurrentTimestampByMill()
	//			//redis处理
	//			for k, v := range req.ReactionExtensionList {
	//				//抢占分布式锁
	//				err:=lockMessageTypeKey(req.ClientMsgID,k)
	//				if err != nil {
	//					setKeyResultInfo(oneFailedReactionExtensionList,100,err.Error(),req.ClientMsgID,k,v)
	//					continue
	//				}
	//				redisValue,err:=db.DB.GetMessageTypeKeyValue(req.ClientMsgID,req.SessionType,k)
	//				if err != nil&&err!=go_redis.Nil {
	//					setKeyResultInfo(oneFailedReactionExtensionList,200,err.Error(),req.ClientMsgID,k,v)
	//					continue
	//				}
	//				temp:=new(server_api_params.KeyValue)
	//				utils.JsonStringToStruct(redisValue,temp)
	//				if v.LatestUpdateTime != temp.LatestUpdateTime {
	//					setKeyResultInfo(oneFailedReactionExtensionList,300,"message have update",req.ClientMsgID,k,temp)
	//					continue
	//				}else{
	//					v.LatestUpdateTime = utils.GetCurrentTimestampByMill()
	//					newerr:=db.DB.SetMessageTypeKeyValue(req.ClientMsgID,req.SessionType,k,utils.StructToJsonString(v))
	//					if newerr != nil {
	//						setKeyResultInfo(oneFailedReactionExtensionList,201,newerr.Error(),req.ClientMsgID,k,temp)
	//						continue
	//					}
	//					setKeyResultInfo(oneSuccessReactionExtensionList,0,"",req.ClientMsgID,k,v)
	//				}
	//
	//			}
	//
	//		}else{
	//			//mongo处理
	//		}
	//
	//	}else{
	//
	//	}
	//	return
	//}
	return

}

func (rpc *rpcChat) AddMessageReactionExtensions(ctx context.Context, req *msg.ModifyMessageReactionExtensionsReq) (resp *msg.ModifyMessageReactionExtensionsResp, err error) {
	return
}

func (rpc *rpcChat) DeleteMessageReactionExtensions(ctx context.Context, req *msg.OperateMessageListReactionExtensionsReq) (resp *msg.OperateMessageListReactionExtensionsResp, err error) {
	return
}
func lockMessageTypeKey(clientMsgID, typeKey string) (err error) {
	for i := 0; i < 3; i++ {
		err = db.DB.LockMessageTypeKey(clientMsgID, typeKey)
		if err != nil {
			time.Sleep(time.Millisecond * 100)
			continue
		} else {
			break
		}
	}
	return err

}
