package handlers

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"strings"

	"github.com/YuriyNasretdinov/social-net/db"
	"github.com/YuriyNasretdinov/social-net/events"
	"github.com/YuriyNasretdinov/social-net/protocol"
)

const (
	DATE_FORMAT = "2006-01-02"
)

type (
	WebsocketCtx struct {
		SeqId    int
		UserId   uint64
		Listener chan interface{}
		UserName string
	}
)

func (ctx *WebsocketCtx) ProcessGetMessages(req *protocol.RequestGetMessages) interface{} {
	dateEnd := req.DateEnd

	if dateEnd == "" {
		dateEnd = fmt.Sprint(time.Now().UnixNano())
	}

	limit := req.Limit
	if limit > protocol.MAX_MESSAGES_LIMIT {
		limit = protocol.MAX_MESSAGES_LIMIT
	}

	if limit <= 0 {
		return &protocol.ResponseError{UserMsg: "Limit must be greater than 0"}
	}

	rows, err := db.GetMessagesStmt.Query(ctx.UserId, req.UserTo, dateEnd, limit)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Cannot select messages", Err: err}
	}

	reply := new(protocol.ReplyGetMessages)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_MESSAGES_LIST"
	reply.Messages = make([]protocol.Message, 0)

	defer rows.Close()
	for rows.Next() {
		var msg protocol.Message
		if err = rows.Scan(&msg.Id, &msg.Text, &msg.Ts, &msg.IsOut); err != nil {
			return &protocol.ResponseError{UserMsg: "Cannot select messages", Err: err}
		}
		msg.UserFrom = fmt.Sprint(req.UserTo)
		reply.Messages = append(reply.Messages, msg)
	}

	return reply
}

func (ctx *WebsocketCtx) ProcessGetUsersList(req *protocol.RequestGetUsersList) interface{} {
	limit := req.Limit
	if limit > protocol.MAX_USERS_LIST_LIMIT {
		limit = protocol.MAX_USERS_LIST_LIMIT
	}

	if limit <= 0 {
		return &protocol.ResponseError{UserMsg: "Limit must be greater than 0"}
	}

	rows, err := db.GetUsersListStmt.Query(limit)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Cannot select users", Err: err}
	}

	reply := new(protocol.ReplyGetUsersList)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_USERS_LIST"
	reply.Users = make([]protocol.JSUserListInfo, 0)

	potentialFriends := make([]string, 0)

	defer rows.Close()
	for rows.Next() {
		var user protocol.JSUserListInfo
		var potentialFriendId int64

		if err = rows.Scan(&user.Name, &potentialFriendId); err != nil {
			return &protocol.ResponseError{UserMsg: "Cannot select users", Err: err}
		}

		user.Id = fmt.Sprint(potentialFriendId)
		reply.Users = append(reply.Users, user)
		potentialFriends = append(potentialFriends, user.Id)
	}

	friendsMap := make(map[string]bool)

	if len(potentialFriends) > 0 {
		friendRows, err := db.Db.Query(`SELECT friend_user_id, request_accepted FROM friend
		WHERE user_id = ` + fmt.Sprint(ctx.UserId) + ` AND friend_user_id IN(` + strings.Join(potentialFriends, ",") + `)`)
		if err != nil {
			return &protocol.ResponseError{UserMsg: "Cannot select users", Err: err}
		}
		defer friendRows.Close()

		for friendRows.Next() {
			var friendId string
			var requestAccepted bool
			if err = friendRows.Scan(&friendId, &requestAccepted); err != nil {
				return &protocol.ResponseError{UserMsg: "Cannot select users", Err: err}
			}

			friendsMap[friendId] = requestAccepted
		}
	}

	for i, user := range reply.Users {
		reply.Users[i].FriendshipConfirmed, reply.Users[i].IsFriend = friendsMap[user.Id]
	}

	return reply
}

func (ctx *WebsocketCtx) ProcessGetFriends(req *protocol.RequestGetFriends) interface{} {
	limit := req.Limit
	if limit > protocol.MAX_FRIENDS_LIMIT {
		limit = protocol.MAX_FRIENDS_LIMIT
	}

	if limit <= 0 {
		return &protocol.ResponseError{UserMsg: "Limit must be greater than 0"}
	}

	friendUserIds, err := getUserFriends(ctx.UserId)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get friends", Err: err}
	}

	reply := new(protocol.ReplyGetFriends)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GET_FRIENDS"
	reply.Users = make([]protocol.JSUserInfo, 0)

	friendUserIdsStr := make([]string, 0)

	for _, userId := range friendUserIds {
		userIdStr := fmt.Sprint(userId)
		reply.Users = append(reply.Users, protocol.JSUserInfo{Id: userIdStr})
		friendUserIdsStr = append(friendUserIdsStr, userIdStr)
	}

	userNames, err := db.GetUserNames(friendUserIdsStr)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get friends", Err: err}
	}

	for i, user := range reply.Users {
		reply.Users[i].Name = userNames[user.Id]
	}

	return reply
}

func (ctx *WebsocketCtx) ProcessGetTimeline(req *protocol.RequestGetTimeline) interface{} {
	dateEnd := req.DateEnd

	if dateEnd == "" {
		dateEnd = fmt.Sprint(time.Now().UnixNano())
	}

	limit := req.Limit
	if limit > protocol.MAX_TIMELINE_LIMIT {
		limit = protocol.MAX_TIMELINE_LIMIT
	}

	if limit <= 0 {
		return &protocol.ResponseError{UserMsg: "Limit must be greater than 0"}
	}

	rows, err := db.GetFromTimelineStmt.Query(ctx.UserId, dateEnd, limit)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Cannot select timeline", Err: err}
	}

	reply := new(protocol.ReplyGetTimeline)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GET_TIMELINE"
	reply.Messages = make([]protocol.TimelineMessage, 0)

	userIds := make([]string, 0)

	defer rows.Close()
	for rows.Next() {
		var msg protocol.TimelineMessage
		if err = rows.Scan(&msg.Id, &msg.UserId, &msg.Text, &msg.Ts); err != nil {
			return &protocol.ResponseError{UserMsg: "Cannot select timeline", Err: err}
		}

		reply.Messages = append(reply.Messages, msg)
		userIds = append(userIds, msg.UserId)
	}

	userNames, err := db.GetUserNames(userIds)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Cannot select timeline", Err: err}
	}

	for i, row := range reply.Messages {
		reply.Messages[i].UserName = userNames[row.UserId]
	}

	return reply
}

func (ctx *WebsocketCtx) ProcessSendMessage(req *protocol.RequestSendMessage) interface{} {
	// TODO: verify that user has rights to send message to the specified person
	var (
		err error
		now = time.Now().UnixNano()
	)

	if len(req.Text) == 0 {
		return &protocol.ResponseError{UserMsg: "Message text must not be empty"}
	}

	_, err = db.SendMessageStmt.Exec(ctx.UserId, req.UserTo, protocol.MSG_TYPE_OUT, req.Text, now)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not log outgoing message", Err: err}
	}

	_, err = db.SendMessageStmt.Exec(req.UserTo, ctx.UserId, protocol.MSG_TYPE_IN, req.Text, now)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not log incoming message", Err: err}
	}

	reply := new(protocol.ReplyGeneric)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GENERIC"
	reply.Success = true

	events.EventsFlow <- &events.ControlEvent{
		EvType:   events.EVENT_NEW_MESSAGE,
		Listener: ctx.Listener,
		Info: &events.InternalEventNewMessage{
			UserFrom: ctx.UserId,
			UserTo:   req.UserTo,
			Ts:       fmt.Sprint(now),
			Text:     req.Text,
		},
	}

	return reply
}

func getUserFriends(userId uint64) (userIds []uint64, err error) {
	res, err := db.GetFriendsList.Query(userId)
	if err != nil {
		return
	}

	defer res.Close()

	userIds = make([]uint64, 0)

	for res.Next() {
		var uid uint64
		if err = res.Scan(&uid); err != nil {
			log.Println(err.Error())
			return
		}

		userIds = append(userIds, uid)
	}

	return
}

func (ctx *WebsocketCtx) ProcessAddToTimeline(req *protocol.RequestAddToTimeline) interface{} {
	var (
		err error
		now = time.Now().UnixNano()
	)

	if len(req.Text) == 0 {
		return &protocol.ResponseError{UserMsg: "Text must not be empty"}
	}

	userIds, err := getUserFriends(ctx.UserId)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get user ids", Err: err}
	}

	userIds = append(userIds, ctx.UserId)

	for _, uid := range userIds {
		if _, err = db.AddToTimelineStmt.Exec(uid, ctx.UserId, req.Text, now); err != nil {
			return &protocol.ResponseError{UserMsg: "Could not add to timeline", Err: err}
		}
	}

	reply := new(protocol.ReplyGeneric)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GENERIC"
	reply.Success = true

	events.EventsFlow <- &events.ControlEvent{
		EvType:   events.EVENT_NEW_TIMELINE_EVENT,
		Listener: ctx.Listener,
		Info: &events.InternalEventNewTimelineStatus{
			UserId:        ctx.UserId,
			FriendUserIds: userIds,
			UserName:      ctx.UserName,
			Ts:            fmt.Sprint(now),
			Text:          req.Text,
		},
	}

	return reply
}

func (ctx *WebsocketCtx) ProcessAddFriend(req *protocol.RequestAddFriend) interface{} {
	var (
		err      error
		friendId uint64
	)

	if friendId, err = strconv.ParseUint(req.FriendId, 10, 64); err != nil {
		return &protocol.ResponseError{UserMsg: "Friend id is not numeric"}
	}

	if friendId == ctx.UserId {
		return &protocol.ResponseError{UserMsg: "You cannot add yourself as a friend"}
	}

	if _, err = db.AddFriendsRequestStmt.Exec(ctx.UserId, friendId, 1); err != nil {
		return &protocol.ResponseError{UserMsg: "Could not add user as a friend", Err: err}
	}

	if _, err = db.AddFriendsRequestStmt.Exec(friendId, ctx.UserId, 0); err != nil {
		return &protocol.ResponseError{UserMsg: "Could not add user as a friend", Err: err}
	}

	reply := new(protocol.ReplyGeneric)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GENERIC"
	reply.Success = true

	return reply
}

func (ctx *WebsocketCtx) ProcessConfirmFriendship(req *protocol.RequestConfirmFriendship) interface{} {
	var (
		err      error
		friendId uint64
	)

	if friendId, err = strconv.ParseUint(req.FriendId, 10, 64); err != nil {
		return &protocol.ResponseError{UserMsg: "Friend id is not numeric"}
	}

	if _, err = db.ConfirmFriendshipStmt.Exec(ctx.UserId, friendId); err != nil {
		return &protocol.ResponseError{UserMsg: "Could not confirm friendship", Err: err}
	}

	reply := new(protocol.ReplyGeneric)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GENERIC"
	reply.Success = true

	return reply
}

func (ctx *WebsocketCtx) ProcessGetMessagesUsers(req *protocol.RequestGetMessagesUsers) interface{} {
	var (
		err error
		id  uint64
		ts  string
	)

	rows, err := db.GetMessagesUsersStmt.Query(ctx.UserId, req.Limit)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get users list for messages", Err: err}
	}

	defer rows.Close()

	reply := new(protocol.ReplyGetMessagesUsers)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GET_MESSAGES_USERS"
	reply.Users = make([]protocol.JSUserInfo, 0)

	userIds := make([]string, 0)
	usersMap := make(map[uint64]bool)

	for rows.Next() {
		if err := rows.Scan(&id, &ts); err != nil {
			return &protocol.ResponseError{UserMsg: "Could not get users list for messages", Err: err}
		}

		usersMap[id] = true

		userId := fmt.Sprint(id)
		reply.Users = append(reply.Users, protocol.JSUserInfo{Id: userId})
		userIds = append(userIds, userId)
	}

	friendIds, err := getUserFriends(ctx.UserId)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get users list for messages", Err: err}
	}

	for _, friendId := range friendIds {
		if usersMap[friendId] {
			continue
		}

		userId := fmt.Sprint(friendId)
		reply.Users = append(reply.Users, protocol.JSUserInfo{Id: userId})
		userIds = append(userIds, userId)
	}

	userNames, err := db.GetUserNames(userIds)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get users list for messages", Err: err}
	}

	for i, user := range reply.Users {
		reply.Users[i].Name = userNames[user.Id]
	}

	return reply
}

func (ctx *WebsocketCtx) ProcessGetProfile(req *protocol.RequestGetProfile) interface{} {
	reply := new(protocol.ReplyGetProfile)
	reply.SeqId = ctx.SeqId
	reply.Type = "REPLY_GET_PROFILE"

	row := db.GetProfileStmt.QueryRow(req.UserId)
	var birthdate time.Time

	err := row.Scan(&reply.Name, &birthdate, &reply.Sex, &reply.Description, &reply.CityId, &reply.FamilyPosition)
	if err != nil {
		return &protocol.ResponseError{UserMsg: "Could not get user profile", Err: err}
	}

	reply.Birthdate = birthdate.Format(DATE_FORMAT)

	city, err := db.GetCityInfo(reply.CityId)
	if err != nil {
		log.Printf("Could not get city by id=%d for user id=%d", reply.CityId, req.UserId)
		city = &db.City{}
	}

	reply.CityName = city.Name
	return reply
}
