package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
)

const panicText = "boom: secret internal detail"

const testUserID = "7f3a1c2e-9999-4e8b-b7c1-2d3e4f5a6b7c"

// fakeAPI answers every operation in the contract with a fixed success, so these tests
// exercise the router and middleware without any module.
type fakeAPI struct{}

func (fakeAPI) OpenWebSocket(context.Context, gen.OpenWebSocketRequestObject) (gen.OpenWebSocketResponseObject, error) {
	return gen.OpenWebSocket101Response{}, nil
}

func (fakeAPI) GetPresence(context.Context, gen.GetPresenceRequestObject) (gen.GetPresenceResponseObject, error) {
	return gen.GetPresence200JSONResponse{Items: []gen.PresenceStatus{}}, nil
}

func (fakeAPI) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: gen.HealthStatusOk, Database: gen.HealthDatabaseOk, Redis: gen.HealthRedisOk}, nil
}

func (fakeAPI) RegisterUser(context.Context, gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	return gen.RegisterUser201JSONResponse{Email: "ada@example.org"}, nil
}

func (fakeAPI) VerifyEmail(context.Context, gen.VerifyEmailRequestObject) (gen.VerifyEmailResponseObject, error) {
	return gen.VerifyEmail204Response{}, nil
}

func (fakeAPI) ResendVerification(context.Context, gen.ResendVerificationRequestObject) (gen.ResendVerificationResponseObject, error) {
	return gen.ResendVerification202Response{}, nil
}

func (fakeAPI) LoginUser(context.Context, gen.LoginUserRequestObject) (gen.LoginUserResponseObject, error) {
	return gen.LoginUser200JSONResponse{Body: gen.CurrentUser{Email: "ada@example.org"}}, nil
}

func (fakeAPI) RefreshSession(context.Context, gen.RefreshSessionRequestObject) (gen.RefreshSessionResponseObject, error) {
	return gen.RefreshSession204Response{}, nil
}

func (fakeAPI) LogoutUser(context.Context, gen.LogoutUserRequestObject) (gen.LogoutUserResponseObject, error) {
	return gen.LogoutUser204Response{}, nil
}

func (fakeAPI) RequestPasswordReset(context.Context, gen.RequestPasswordResetRequestObject) (gen.RequestPasswordResetResponseObject, error) {
	return gen.RequestPasswordReset202Response{}, nil
}

func (fakeAPI) ConfirmPasswordReset(context.Context, gen.ConfirmPasswordResetRequestObject) (gen.ConfirmPasswordResetResponseObject, error) {
	return gen.ConfirmPasswordReset204Response{}, nil
}

func (fakeAPI) ChangePassword(context.Context, gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	return gen.ChangePassword204Response{}, nil
}

func (fakeAPI) DeleteAccount(context.Context, gen.DeleteAccountRequestObject) (gen.DeleteAccountResponseObject, error) {
	return gen.DeleteAccount204Response{}, nil
}

func (fakeAPI) ListUsers(context.Context, gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	return gen.ListUsers200JSONResponse{Items: []gen.AdminUser{}}, nil
}

func (fakeAPI) GetMyProfile(context.Context, gen.GetMyProfileRequestObject) (gen.GetMyProfileResponseObject, error) {
	return gen.GetMyProfile200JSONResponse{}, nil
}

func (fakeAPI) UpdateMyProfile(context.Context, gen.UpdateMyProfileRequestObject) (gen.UpdateMyProfileResponseObject, error) {
	return gen.UpdateMyProfile200JSONResponse{}, nil
}

func (fakeAPI) GetProfile(context.Context, gen.GetProfileRequestObject) (gen.GetProfileResponseObject, error) {
	return gen.GetProfile200JSONResponse{}, nil
}

func (fakeAPI) AddMyInterest(context.Context, gen.AddMyInterestRequestObject) (gen.AddMyInterestResponseObject, error) {
	return gen.AddMyInterest201JSONResponse{}, nil
}

func (fakeAPI) RemoveMyInterest(context.Context, gen.RemoveMyInterestRequestObject) (gen.RemoveMyInterestResponseObject, error) {
	return gen.RemoveMyInterest204Response{}, nil
}

func (fakeAPI) AddMySkill(context.Context, gen.AddMySkillRequestObject) (gen.AddMySkillResponseObject, error) {
	return gen.AddMySkill201JSONResponse{}, nil
}

func (fakeAPI) RemoveMySkill(context.Context, gen.RemoveMySkillRequestObject) (gen.RemoveMySkillResponseObject, error) {
	return gen.RemoveMySkill204Response{}, nil
}

func (fakeAPI) AddMyEducation(context.Context, gen.AddMyEducationRequestObject) (gen.AddMyEducationResponseObject, error) {
	return gen.AddMyEducation201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyEducation(context.Context, gen.UpdateMyEducationRequestObject) (gen.UpdateMyEducationResponseObject, error) {
	return gen.UpdateMyEducation200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyEducation(context.Context, gen.RemoveMyEducationRequestObject) (gen.RemoveMyEducationResponseObject, error) {
	return gen.RemoveMyEducation204Response{}, nil
}

func (fakeAPI) AddMyExperience(context.Context, gen.AddMyExperienceRequestObject) (gen.AddMyExperienceResponseObject, error) {
	return gen.AddMyExperience201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyExperience(context.Context, gen.UpdateMyExperienceRequestObject) (gen.UpdateMyExperienceResponseObject, error) {
	return gen.UpdateMyExperience200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyExperience(context.Context, gen.RemoveMyExperienceRequestObject) (gen.RemoveMyExperienceResponseObject, error) {
	return gen.RemoveMyExperience204Response{}, nil
}

func (fakeAPI) ListMyPublications(context.Context, gen.ListMyPublicationsRequestObject) (gen.ListMyPublicationsResponseObject, error) {
	return gen.ListMyPublications200JSONResponse{}, nil
}

func (fakeAPI) ListPublications(context.Context, gen.ListPublicationsRequestObject) (gen.ListPublicationsResponseObject, error) {
	return gen.ListPublications200JSONResponse{}, nil
}

func (fakeAPI) AddMyPublication(context.Context, gen.AddMyPublicationRequestObject) (gen.AddMyPublicationResponseObject, error) {
	return gen.AddMyPublication201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyPublication(context.Context, gen.UpdateMyPublicationRequestObject) (gen.UpdateMyPublicationResponseObject, error) {
	return gen.UpdateMyPublication200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyPublication(context.Context, gen.RemoveMyPublicationRequestObject) (gen.RemoveMyPublicationResponseObject, error) {
	return gen.RemoveMyPublication204Response{}, nil
}

func (fakeAPI) LookupPublicationByDOI(context.Context, gen.LookupPublicationByDOIRequestObject) (gen.LookupPublicationByDOIResponseObject, error) {
	return gen.LookupPublicationByDOI200JSONResponse{}, nil
}

func (fakeAPI) AddMyPublicationFromDOI(context.Context, gen.AddMyPublicationFromDOIRequestObject) (gen.AddMyPublicationFromDOIResponseObject, error) {
	return gen.AddMyPublicationFromDOI201JSONResponse{}, nil
}

func (fakeAPI) SetMyOrcidId(context.Context, gen.SetMyOrcidIdRequestObject) (gen.SetMyOrcidIdResponseObject, error) {
	return gen.SetMyOrcidId200JSONResponse{}, nil
}

func (fakeAPI) ListMyOrcidWorks(context.Context, gen.ListMyOrcidWorksRequestObject) (gen.ListMyOrcidWorksResponseObject, error) {
	return gen.ListMyOrcidWorks200JSONResponse{}, nil
}

func (fakeAPI) ImportMyOrcidWorks(context.Context, gen.ImportMyOrcidWorksRequestObject) (gen.ImportMyOrcidWorksResponseObject, error) {
	return gen.ImportMyOrcidWorks200JSONResponse{}, nil
}

func (fakeAPI) AddMyLanguage(context.Context, gen.AddMyLanguageRequestObject) (gen.AddMyLanguageResponseObject, error) {
	return gen.AddMyLanguage201JSONResponse{}, nil
}

func (fakeAPI) UpdateMyLanguage(context.Context, gen.UpdateMyLanguageRequestObject) (gen.UpdateMyLanguageResponseObject, error) {
	return gen.UpdateMyLanguage200JSONResponse{}, nil
}

func (fakeAPI) RemoveMyLanguage(context.Context, gen.RemoveMyLanguageRequestObject) (gen.RemoveMyLanguageResponseObject, error) {
	return gen.RemoveMyLanguage204Response{}, nil
}

func (fakeAPI) GetMyContact(context.Context, gen.GetMyContactRequestObject) (gen.GetMyContactResponseObject, error) {
	return gen.GetMyContact200JSONResponse{}, nil
}

func (fakeAPI) UpdateMyContact(context.Context, gen.UpdateMyContactRequestObject) (gen.UpdateMyContactResponseObject, error) {
	return gen.UpdateMyContact200JSONResponse{}, nil
}

func (fakeAPI) AddMyLink(context.Context, gen.AddMyLinkRequestObject) (gen.AddMyLinkResponseObject, error) {
	return gen.AddMyLink201JSONResponse{}, nil
}

func (fakeAPI) RemoveMyLink(context.Context, gen.RemoveMyLinkRequestObject) (gen.RemoveMyLinkResponseObject, error) {
	return gen.RemoveMyLink204Response{}, nil
}

func (fakeAPI) UploadMyPhoto(context.Context, gen.UploadMyPhotoRequestObject) (gen.UploadMyPhotoResponseObject, error) {
	return gen.UploadMyPhoto200JSONResponse{ProfileImageStoredJSONResponse: gen.ProfileImageStoredJSONResponse{Url: "/api/v1/media/x"}}, nil
}

func (fakeAPI) RemoveMyPhoto(context.Context, gen.RemoveMyPhotoRequestObject) (gen.RemoveMyPhotoResponseObject, error) {
	return gen.RemoveMyPhoto204Response{}, nil
}

func (fakeAPI) UploadMyCover(context.Context, gen.UploadMyCoverRequestObject) (gen.UploadMyCoverResponseObject, error) {
	return gen.UploadMyCover200JSONResponse{ProfileImageStoredJSONResponse: gen.ProfileImageStoredJSONResponse{Url: "/api/v1/media/x"}}, nil
}

func (fakeAPI) RemoveMyCover(context.Context, gen.RemoveMyCoverRequestObject) (gen.RemoveMyCoverResponseObject, error) {
	return gen.RemoveMyCover204Response{}, nil
}

func (fakeAPI) GetMedia(context.Context, gen.GetMediaRequestObject) (gen.GetMediaResponseObject, error) {
	return gen.GetMedia302Response{Headers: gen.GetMedia302ResponseHeaders{Location: "http://localhost/x", CacheControl: "private, max-age=300"}}, nil
}

func (fakeAPI) SearchPeople(context.Context, gen.SearchPeopleRequestObject) (gen.SearchPeopleResponseObject, error) {
	return gen.SearchPeople200JSONResponse{}, nil
}

func (fakeAPI) SendConnectionRequest(context.Context, gen.SendConnectionRequestRequestObject) (gen.SendConnectionRequestResponseObject, error) {
	return gen.SendConnectionRequest201JSONResponse{}, nil
}

func (fakeAPI) AcceptConnectionRequest(context.Context, gen.AcceptConnectionRequestRequestObject) (gen.AcceptConnectionRequestResponseObject, error) {
	return gen.AcceptConnectionRequest200JSONResponse{}, nil
}

func (fakeAPI) RejectConnectionRequest(context.Context, gen.RejectConnectionRequestRequestObject) (gen.RejectConnectionRequestResponseObject, error) {
	return gen.RejectConnectionRequest204Response{}, nil
}

func (fakeAPI) CancelConnectionRequest(context.Context, gen.CancelConnectionRequestRequestObject) (gen.CancelConnectionRequestResponseObject, error) {
	return gen.CancelConnectionRequest204Response{}, nil
}

func (fakeAPI) RemoveConnection(context.Context, gen.RemoveConnectionRequestObject) (gen.RemoveConnectionResponseObject, error) {
	return gen.RemoveConnection204Response{}, nil
}

func (fakeAPI) ListMyConnections(context.Context, gen.ListMyConnectionsRequestObject) (gen.ListMyConnectionsResponseObject, error) {
	return gen.ListMyConnections200JSONResponse{}, nil
}

func (fakeAPI) ListReceivedConnectionRequests(context.Context, gen.ListReceivedConnectionRequestsRequestObject) (gen.ListReceivedConnectionRequestsResponseObject, error) {
	return gen.ListReceivedConnectionRequests200JSONResponse{}, nil
}

func (fakeAPI) ListSentConnectionRequests(context.Context, gen.ListSentConnectionRequestsRequestObject) (gen.ListSentConnectionRequestsResponseObject, error) {
	return gen.ListSentConnectionRequests200JSONResponse{}, nil
}

func (fakeAPI) ListNotifications(context.Context, gen.ListNotificationsRequestObject) (gen.ListNotificationsResponseObject, error) {
	return gen.ListNotifications200JSONResponse{}, nil
}

func (fakeAPI) GetUnreadNotificationCount(context.Context, gen.GetUnreadNotificationCountRequestObject) (gen.GetUnreadNotificationCountResponseObject, error) {
	return gen.GetUnreadNotificationCount200JSONResponse{}, nil
}

func (fakeAPI) MarkNotificationRead(context.Context, gen.MarkNotificationReadRequestObject) (gen.MarkNotificationReadResponseObject, error) {
	return gen.MarkNotificationRead204Response{}, nil
}

func (fakeAPI) MarkAllNotificationsRead(context.Context, gen.MarkAllNotificationsReadRequestObject) (gen.MarkAllNotificationsReadResponseObject, error) {
	return gen.MarkAllNotificationsRead200JSONResponse{}, nil
}

func (fakeAPI) SendDirectMessage(context.Context, gen.SendDirectMessageRequestObject) (gen.SendDirectMessageResponseObject, error) {
	return gen.SendDirectMessage201JSONResponse{}, nil
}

func (fakeAPI) SendMessage(context.Context, gen.SendMessageRequestObject) (gen.SendMessageResponseObject, error) {
	return gen.SendMessage201JSONResponse{}, nil
}

func (fakeAPI) BlockUser(context.Context, gen.BlockUserRequestObject) (gen.BlockUserResponseObject, error) {
	return gen.BlockUser204Response{}, nil
}

func (fakeAPI) UnblockUser(context.Context, gen.UnblockUserRequestObject) (gen.UnblockUserResponseObject, error) {
	return gen.UnblockUser204Response{}, nil
}

func (fakeAPI) ListBlockedPeople(context.Context, gen.ListBlockedPeopleRequestObject) (gen.ListBlockedPeopleResponseObject, error) {
	return gen.ListBlockedPeople200JSONResponse{}, nil
}

func (fakeAPI) ReportConversation(context.Context, gen.ReportConversationRequestObject) (gen.ReportConversationResponseObject, error) {
	return gen.ReportConversation201JSONResponse{}, nil
}

func (fakeAPI) ListMessageReports(context.Context, gen.ListMessageReportsRequestObject) (gen.ListMessageReportsResponseObject, error) {
	return gen.ListMessageReports200JSONResponse{}, nil
}

func (fakeAPI) GetMessageReport(context.Context, gen.GetMessageReportRequestObject) (gen.GetMessageReportResponseObject, error) {
	return gen.GetMessageReport200JSONResponse{}, nil
}

func (fakeAPI) ResolveMessageReport(context.Context, gen.ResolveMessageReportRequestObject) (gen.ResolveMessageReportResponseObject, error) {
	return gen.ResolveMessageReport200JSONResponse{}, nil
}

func (fakeAPI) GetMessageUnreadCount(context.Context, gen.GetMessageUnreadCountRequestObject) (gen.GetMessageUnreadCountResponseObject, error) {
	return gen.GetMessageUnreadCount200JSONResponse{}, nil
}

func (fakeAPI) SearchMessages(context.Context, gen.SearchMessagesRequestObject) (gen.SearchMessagesResponseObject, error) {
	return gen.SearchMessages200JSONResponse{}, nil
}

func (fakeAPI) ListConversations(context.Context, gen.ListConversationsRequestObject) (gen.ListConversationsResponseObject, error) {
	return gen.ListConversations200JSONResponse{}, nil
}

func (fakeAPI) GetConversation(context.Context, gen.GetConversationRequestObject) (gen.GetConversationResponseObject, error) {
	return gen.GetConversation200JSONResponse{}, nil
}

func (fakeAPI) ListMessages(context.Context, gen.ListMessagesRequestObject) (gen.ListMessagesResponseObject, error) {
	return gen.ListMessages200JSONResponse{}, nil
}

func (fakeAPI) MarkConversationRead(context.Context, gen.MarkConversationReadRequestObject) (gen.MarkConversationReadResponseObject, error) {
	return gen.MarkConversationRead204Response{}, nil
}

func (fakeAPI) EditMessage(context.Context, gen.EditMessageRequestObject) (gen.EditMessageResponseObject, error) {
	return gen.EditMessage200JSONResponse{}, nil
}

func (fakeAPI) UploadAttachment(context.Context, gen.UploadAttachmentRequestObject) (gen.UploadAttachmentResponseObject, error) {
	return gen.UploadAttachment201JSONResponse{}, nil
}

func (fakeAPI) GetAttachment(context.Context, gen.GetAttachmentRequestObject) (gen.GetAttachmentResponseObject, error) {
	return gen.GetAttachment302Response{Headers: gen.AttachmentRedirectResponseHeaders{Location: "http://localhost/x", CacheControl: "no-store", XContentTypeOptions: "nosniff"}}, nil
}

func (fakeAPI) GetReportedAttachment(context.Context, gen.GetReportedAttachmentRequestObject) (gen.GetReportedAttachmentResponseObject, error) {
	return gen.GetReportedAttachment302Response{Headers: gen.AttachmentRedirectResponseHeaders{Location: "http://localhost/x", CacheControl: "no-store", XContentTypeOptions: "nosniff"}}, nil
}

func (fakeAPI) DeleteMessage(context.Context, gen.DeleteMessageRequestObject) (gen.DeleteMessageResponseObject, error) {
	return gen.DeleteMessage204Response{}, nil
}

func (fakeAPI) AcceptMessageRequest(context.Context, gen.AcceptMessageRequestRequestObject) (gen.AcceptMessageRequestResponseObject, error) {
	return gen.AcceptMessageRequest200JSONResponse{}, nil
}

func (fakeAPI) DeclineMessageRequest(context.Context, gen.DeclineMessageRequestRequestObject) (gen.DeclineMessageRequestResponseObject, error) {
	return gen.DeclineMessageRequest200JSONResponse{}, nil
}

func (fakeAPI) ListProfileConnections(context.Context, gen.ListProfileConnectionsRequestObject) (gen.ListProfileConnectionsResponseObject, error) {
	return gen.ListProfileConnections200JSONResponse{}, nil
}

func (fakeAPI) SuggestInterestTags(context.Context, gen.SuggestInterestTagsRequestObject) (gen.SuggestInterestTagsResponseObject, error) {
	return gen.SuggestInterestTags200JSONResponse{Items: []gen.Tag{}}, nil
}

func (fakeAPI) SuggestSkillTags(context.Context, gen.SuggestSkillTagsRequestObject) (gen.SuggestSkillTagsResponseObject, error) {
	return gen.SuggestSkillTags200JSONResponse{Items: []gen.Tag{}}, nil
}

// GetCurrentUser echoes the user ID that the auth middleware put in the context.
func (fakeAPI) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	id, _ := httpx.UserID(ctx)
	return gen.GetCurrentUser200JSONResponse{FullName: id}, nil
}

func (fakeAPI) WarnUser(context.Context, gen.WarnUserRequestObject) (gen.WarnUserResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RestrictUser(context.Context, gen.RestrictUserRequestObject) (gen.RestrictUserResponseObject, error) {
	return nil, nil
}

func (fakeAPI) LiftRestriction(context.Context, gen.LiftRestrictionRequestObject) (gen.LiftRestrictionResponseObject, error) {
	return nil, nil
}

func (fakeAPI) SuspendUser(context.Context, gen.SuspendUserRequestObject) (gen.SuspendUserResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RestoreUser(context.Context, gen.RestoreUserRequestObject) (gen.RestoreUserResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetUserModeration(context.Context, gen.GetUserModerationRequestObject) (gen.GetUserModerationResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetEmbeddingStats(context.Context, gen.GetEmbeddingStatsRequestObject) (gen.GetEmbeddingStatsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RebuildEmbeddings(context.Context, gen.RebuildEmbeddingsRequestObject) (gen.RebuildEmbeddingsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListPendingWarnings(context.Context, gen.ListPendingWarningsRequestObject) (gen.ListPendingWarningsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetMyWarning(context.Context, gen.GetMyWarningRequestObject) (gen.GetMyWarningResponseObject, error) {
	return nil, nil
}

func (fakeAPI) AcknowledgeWarning(context.Context, gen.AcknowledgeWarningRequestObject) (gen.AcknowledgeWarningResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetMySettings(context.Context, gen.GetMySettingsRequestObject) (gen.GetMySettingsResponseObject, error) {
	return gen.GetMySettings200JSONResponse{}, nil
}

func (fakeAPI) UpdateMySettings(context.Context, gen.UpdateMySettingsRequestObject) (gen.UpdateMySettingsResponseObject, error) {
	return gen.UpdateMySettings200JSONResponse{}, nil
}

func (fakeAPI) UnsubscribeDigest(context.Context, gen.UnsubscribeDigestRequestObject) (gen.UnsubscribeDigestResponseObject, error) {
	return gen.UnsubscribeDigest204Response{}, nil
}

func (fakeAPI) CreateGroup(context.Context, gen.CreateGroupRequestObject) (gen.CreateGroupResponseObject, error) {
	return gen.CreateGroup201JSONResponse{}, nil
}

func (fakeAPI) RenameGroup(context.Context, gen.RenameGroupRequestObject) (gen.RenameGroupResponseObject, error) {
	return gen.RenameGroup200JSONResponse{}, nil
}

func (fakeAPI) AddGroupMembers(context.Context, gen.AddGroupMembersRequestObject) (gen.AddGroupMembersResponseObject, error) {
	return gen.AddGroupMembers200JSONResponse{}, nil
}

func (fakeAPI) RemoveGroupMember(context.Context, gen.RemoveGroupMemberRequestObject) (gen.RemoveGroupMemberResponseObject, error) {
	return gen.RemoveGroupMember204Response{}, nil
}

func (fakeAPI) LeaveGroup(context.Context, gen.LeaveGroupRequestObject) (gen.LeaveGroupResponseObject, error) {
	return gen.LeaveGroup204Response{}, nil
}

func (fakeAPI) RecommendPeople(context.Context, gen.RecommendPeopleRequestObject) (gen.RecommendPeopleResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RecommendProjects(context.Context, gen.RecommendProjectsRequestObject) (gen.RecommendProjectsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RecommendProjectResearchers(context.Context, gen.RecommendProjectResearchersRequestObject) (gen.RecommendProjectResearchersResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CreateProject(context.Context, gen.CreateProjectRequestObject) (gen.CreateProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetProject(context.Context, gen.GetProjectRequestObject) (gen.GetProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) UpdateProject(context.Context, gen.UpdateProjectRequestObject) (gen.UpdateProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) DeleteProject(context.Context, gen.DeleteProjectRequestObject) (gen.DeleteProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ReportProject(context.Context, gen.ReportProjectRequestObject) (gen.ReportProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListMyProjects(context.Context, gen.ListMyProjectsRequestObject) (gen.ListMyProjectsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CreateJoinRequest(context.Context, gen.CreateJoinRequestRequestObject) (gen.CreateJoinRequestResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CancelJoinRequest(context.Context, gen.CancelJoinRequestRequestObject) (gen.CancelJoinRequestResponseObject, error) {
	return nil, nil
}

func (fakeAPI) AcceptJoinRequest(context.Context, gen.AcceptJoinRequestRequestObject) (gen.AcceptJoinRequestResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RejectJoinRequest(context.Context, gen.RejectJoinRequestRequestObject) (gen.RejectJoinRequestResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListProjectJoinRequests(context.Context, gen.ListProjectJoinRequestsRequestObject) (gen.ListProjectJoinRequestsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CreateInvitation(context.Context, gen.CreateInvitationRequestObject) (gen.CreateInvitationResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListProjectInvitations(context.Context, gen.ListProjectInvitationsRequestObject) (gen.ListProjectInvitationsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CancelInvitation(context.Context, gen.CancelInvitationRequestObject) (gen.CancelInvitationResponseObject, error) {
	return nil, nil
}

func (fakeAPI) AcceptInvitation(context.Context, gen.AcceptInvitationRequestObject) (gen.AcceptInvitationResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RejectInvitation(context.Context, gen.RejectInvitationRequestObject) (gen.RejectInvitationResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListProjectMembers(context.Context, gen.ListProjectMembersRequestObject) (gen.ListProjectMembersResponseObject, error) {
	return nil, nil
}

func (fakeAPI) UpdateProjectMember(context.Context, gen.UpdateProjectMemberRequestObject) (gen.UpdateProjectMemberResponseObject, error) {
	return nil, nil
}

func (fakeAPI) RemoveProjectMember(context.Context, gen.RemoveProjectMemberRequestObject) (gen.RemoveProjectMemberResponseObject, error) {
	return nil, nil
}

func (fakeAPI) LeaveProject(context.Context, gen.LeaveProjectRequestObject) (gen.LeaveProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) TransferProject(context.Context, gen.TransferProjectRequestObject) (gen.TransferProjectResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListProjectFiles(context.Context, gen.ListProjectFilesRequestObject) (gen.ListProjectFilesResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CreateProjectFile(context.Context, gen.CreateProjectFileRequestObject) (gen.CreateProjectFileResponseObject, error) {
	return nil, nil
}

func (fakeAPI) StartFigureUpload(context.Context, gen.StartFigureUploadRequestObject) (gen.StartFigureUploadResponseObject, error) {
	return nil, nil
}

func (fakeAPI) CompleteFigureUpload(context.Context, gen.CompleteFigureUploadRequestObject) (gen.CompleteFigureUploadResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetProjectFile(context.Context, gen.GetProjectFileRequestObject) (gen.GetProjectFileResponseObject, error) {
	return nil, nil
}

func (fakeAPI) MoveProjectFile(context.Context, gen.MoveProjectFileRequestObject) (gen.MoveProjectFileResponseObject, error) {
	return nil, nil
}

func (fakeAPI) DeleteProjectFile(context.Context, gen.DeleteProjectFileRequestObject) (gen.DeleteProjectFileResponseObject, error) {
	return nil, nil
}

func (fakeAPI) SaveProjectFileContent(context.Context, gen.SaveProjectFileContentRequestObject) (gen.SaveProjectFileContentResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListMyJoinRequests(context.Context, gen.ListMyJoinRequestsRequestObject) (gen.ListMyJoinRequestsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListMyInvitations(context.Context, gen.ListMyInvitationsRequestObject) (gen.ListMyInvitationsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) SearchProjects(context.Context, gen.SearchProjectsRequestObject) (gen.SearchProjectsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ListProjectReports(context.Context, gen.ListProjectReportsRequestObject) (gen.ListProjectReportsResponseObject, error) {
	return nil, nil
}

func (fakeAPI) GetProjectReport(context.Context, gen.GetProjectReportRequestObject) (gen.GetProjectReportResponseObject, error) {
	return nil, nil
}

func (fakeAPI) ResolveProjectReport(context.Context, gen.ResolveProjectReportRequestObject) (gen.ResolveProjectReportResponseObject, error) {
	return nil, nil
}

var _ gen.StrictServerInterface = fakeAPI{}

var testSigner = accesstoken.New([]byte("test-secret-that-is-at-least-32-bytes-long"), 15*time.Minute)

// newTestRouter returns the full router with a fake API, a /test/panic route, and a
// buffer that receives the JSON logs.
func newTestRouter(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(logging.NewHandler(slog.NewJSONHandler(&logs, nil)))
	router := newRouter(logger, fakeAPI{}, httpx.RequireAuth(testSigner, time.Now), func(mux *http.ServeMux) {
		mux.HandleFunc("GET /test/panic", func(http.ResponseWriter, *http.Request) { panic(panicText) })
	})
	return router, &logs
}

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	code, _ = body["code"].(string)
	message, _ = body["message"].(string)
	delete(body, "field") // optional
	if len(body) != 2 {
		t.Errorf("body has keys %v, want code, message and at most field", body)
	}
	if code == "" || message == "" {
		t.Errorf("body %v lacks a code or message", body)
	}
	return code, message
}

func TestErrorShape(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
		wantAllow  string
	}{
		{"unknown route", http.MethodGet, "/nope", http.StatusNotFound, "not_found", ""},
		{"unknown route under api", http.MethodGet, "/api/v1/nope", http.StatusNotFound, "not_found", ""},
		{"root", http.MethodGet, "/", http.StatusNotFound, "not_found", ""},
		{"wrong method", http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD"},
		{"panic", http.MethodGet, "/test/panic", http.StatusInternalServerError, "internal_error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)
			rec := serve(t, router, tt.method, tt.path)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			code, _ := decodeError(t, rec)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("body leaks the panic value: %s", rec.Body)
			}
		})
	}
}

func TestUnknownBodyFieldIsRefused(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantMessage string
	}{
		{"known fields", `{"email":"ada@example.org","password":"secret"}`, http.StatusOK, ""},
		{"unknown field", `{"email":"ada@example.org","password":"secret","orcidId":"x"}`,
			http.StatusBadRequest, `The request has an unknown field "orcidId".`},
		{"not JSON", `{`, http.StatusBadRequest, "The request body is not valid JSON."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusOK {
				return
			}
			code, message := decodeError(t, rec)
			if code != "bad_request" || message != tt.wantMessage {
				t.Errorf("got %q %q, want bad_request %q", code, message, tt.wantMessage)
			}
		})
	}
}

// postJSON posts a JSON body with a session and returns the decoded answer too.
func postJSON(t *testing.T, router http.Handler, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	token, _, err := testSigner.Sign(testUserID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	router.ServeHTTP(rec, req)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return rec, got
}

func TestBodyErrorsNameTheField(t *testing.T) {
	const (
		user   = "7f3a1c2e-1111-4e8b-b7c1-2d3e4f5a6b7c"
		client = "7f3a1c2e-5555-4e8b-b7c1-2d3e4f5a6b7c"
	)
	tests := []struct {
		name, body, message string
		field               any // nil when no field is at fault
	}{
		{"bad format", `{"userId":"` + user + `","body":"hi","clientId":"c1"}`, "clientId must be a UUID.", "clientId"},
		{"wrong type", `{"userId":"` + user + `","body":5,"clientId":"` + client + `"}`, "body must be a string.", "body"},
		{"missing required", `{"userId":"` + user + `","clientId":"` + client + `"}`, "body is required.", "body"},
		{"unknown field", `{"userId":"` + user + `","body":"hi","clientId":"` + client + `","x":1}`, `The request has an unknown field "x".`, "x"},
		{"malformed JSON", `{"userId":`, "The request body is not valid JSON.", nil},
		{"more than one value", `{} {}`, "The request body is not valid JSON.", nil},
		{"not an object", `[1]`, "The request body must be a JSON object.", nil},
		{"empty", ``, "The request body is required.", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)
			rec, got := postJSON(t, router, "/api/v1/messages/direct", tt.body)
			if rec.Code != http.StatusBadRequest || got["code"] != "bad_request" || got["message"] != tt.message || got["field"] != tt.field {
				t.Errorf("got %d %v; want message %q, field %v", rec.Code, got, tt.message, tt.field)
			}
			if _, present := got["field"]; present != (tt.field != nil) {
				t.Errorf("field present = %v in %v", present, got)
			}
		})
	}
}

func TestParameterErrorsNameTheParameter(t *testing.T) {
	router, _ := newTestRouter(t)
	token, _, err := testSigner.Sign(testUserID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/conversations?limit=abc", "/api/v1/notifications?limit=1.5", "/api/v1/conversations/x/messages?limit=abc"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
		router.ServeHTTP(rec, req)
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusBadRequest || got["code"] != "bad_request" || got["field"] != "limit" || got["message"] != "limit must be a whole number." {
			t.Errorf("%s: %d %v", path, rec.Code, got)
		}
	}
}

func TestParamError(t *testing.T) {
	_, parseInt := strconv.Atoi("x")
	_, parseBool := strconv.ParseBool("x")
	tests := []struct {
		name string
		err  error
		want string // the sentence, empty for no FieldError
	}{
		{"required", &gen.RequiredParamError{ParamName: "q"}, "q is required."},
		{"whole number", &gen.InvalidParamFormatError{ParamName: "page", Err: fmt.Errorf("binding: %w", parseInt)}, "page must be a whole number."},
		{"boolean", &gen.InvalidParamFormatError{ParamName: "all", Err: parseBool}, "all must be true or false."},
		{"anything else", &gen.InvalidParamFormatError{ParamName: "at", Err: errors.New("bad secret-value")}, "at is not valid."},
		{"another kind of error", errors.New("bad"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paramError(tt.err)
			if (got == nil) != (tt.want == "") || (got != nil && got.Error() != tt.want) {
				t.Errorf("got %v, want %q", got, tt.want)
			}
		})
	}
}

// What a client sent reaches neither the answer nor the logs.
func TestBadRequestsNeverRepeatTheValue(t *testing.T) {
	const secret = "hunter2secret"
	router, logs := newTestRouter(t)
	bodies := []string{
		`{"userId":"` + secret + `","body":"hi","clientId":"c1"}`,
		`{"userId":5,"body":"` + secret + `"}`,
		`{"body":"` + secret,
		secret,
	}
	for _, body := range bodies {
		rec, got := postJSON(t, router, "/api/v1/messages/direct", body)
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), secret) {
			t.Errorf("%s: answer %d %v", body, rec.Code, got)
		}
	}
	token, _, err := testSigner.Sign(testUserID, "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/conversations?limit=1"+secret, nil)
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	router.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("parameter: %s", rec.Body)
	}
	if strings.Contains(logs.String(), secret) {
		t.Errorf("logs repeat the value: %s", logs)
	}
}

func TestRequestIDIsSetAndUnique(t *testing.T) {
	router, _ := newTestRouter(t)
	seen := map[string]bool{}
	for range 3 {
		id := serve(t, router, http.MethodGet, "/api/v1/health").Header().Get(httpx.RequestIDHeader)
		if len(id) != 32 {
			t.Errorf("request ID %q, want 32 hex characters", id)
		}
		if seen[id] {
			t.Errorf("request ID %q repeated", id)
		}
		seen[id] = true
	}
}

func TestClientRequestIDIsIgnored(t *testing.T) {
	router, _ := newTestRouter(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/health", nil)
	req.Header.Set(httpx.RequestIDHeader, "injected\nvalue")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if got := rec.Header().Get(httpx.RequestIDHeader); got == "injected\nvalue" || len(got) != 32 {
		t.Errorf("request ID = %q, want a new server-made ID", got)
	}
}

func TestOneAccessLogLinePerRequest(t *testing.T) {
	tests := []struct {
		method, path string
		wantStatus   float64
	}{
		{http.MethodGet, "/api/v1/health?token=abc", 200},
		{http.MethodGet, "/nope", 404},
		{http.MethodGet, "/test/panic", 500},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			router, logs := newTestRouter(t)
			rec := serve(t, router, tt.method, tt.path)

			var access []map[string]any
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatalf("log line %q is not JSON: %v", line, err)
				}
				if entry["msg"] == "request" {
					access = append(access, entry)
				}
			}
			if len(access) != 1 {
				t.Fatalf("got %d access log lines, want 1:\n%s", len(access), logs)
			}
			e := access[0]
			wantPath, _, _ := strings.Cut(tt.path, "?")
			if e["method"] != tt.method || e["path"] != wantPath || e["status"] != tt.wantStatus {
				t.Errorf("access line = %v, want method %s path %s status %v", e, tt.method, wantPath, tt.wantStatus)
			}
			if _, ok := e["duration_ms"].(float64); !ok {
				t.Errorf("access line lacks duration_ms: %v", e)
			}
			if e["request_id"] != rec.Header().Get(httpx.RequestIDHeader) {
				t.Errorf("request_id = %v, want %q", e["request_id"], rec.Header().Get(httpx.RequestIDHeader))
			}
			if strings.Contains(logs.String(), "token=abc") {
				t.Errorf("logs contain the query string:\n%s", logs)
			}
		})
	}
}

func TestPanicIsLoggedWithRequestID(t *testing.T) {
	router, logs := newTestRouter(t)
	rec := serve(t, router, http.MethodGet, "/test/panic")
	out := logs.String()
	if !strings.Contains(out, "panic recovered") || !strings.Contains(out, panicText) {
		t.Errorf("panic not logged:\n%s", out)
	}
	if !strings.Contains(out, `"request_id":"`+rec.Header().Get(httpx.RequestIDHeader)+`"`) {
		t.Errorf("panic log lacks the request ID:\n%s", out)
	}
}

// The contract secures every operation without `security: []`; the generated wrapper
// tells the auth middleware so, and public operations need no cookie.
func TestContractDecidesWhichOperationsNeedASession(t *testing.T) {
	router, _ := newTestRouter(t)

	rec := serve(t, router, http.MethodGet, "/api/v1/auth/me")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /auth/me without a cookie: status = %d, want 401", rec.Code)
	}
	if code, _ := decodeError(t, rec); code != httpx.CodeUnauthenticated {
		t.Errorf("code = %q, want %q", code, httpx.CodeUnauthenticated)
	}

	token, _, err := testSigner.Sign("user-1", "researcher", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user-1"`) {
		t.Errorf("GET /auth/me with a valid cookie: %d %s, want 200 with the user ID", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/password",
		strings.NewReader(`{"currentPassword":"password1","newPassword":"password2"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /auth/password without a cookie: status = %d, want 401", rec.Code)
	}

	if rec := serve(t, router, http.MethodGet, "/api/v1/health"); rec.Code != http.StatusOK {
		t.Errorf("GET /health without a cookie: status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"ada@example.org","password":"pw"}`)))
	if rec.Code != http.StatusOK {
		t.Errorf("POST /auth/login without a cookie: status = %d, want 200", rec.Code)
	}
	for _, path := range []string{"/api/v1/auth/refresh", "/api/v1/auth/logout"} {
		if rec := serve(t, router, http.MethodPost, path); rec.Code != http.StatusNoContent {
			t.Errorf("POST %s without an access cookie: status = %d, want 204", path, rec.Code)
		}
	}
}

func TestRoutes(t *testing.T) {
	got := Routes()
	if len(got) != 145 {
		t.Errorf("Routes() returned %d routes, want the 145 operations in the contract: %v", len(got), got)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("route %q registered twice", p)
		}
		seen[p] = true
	}
	for _, want := range []string{"GET /api/v1/health", "GET /api/v1/auth/me", "POST /api/v1/account/delete", "POST /api/v1/admin/restrictions/{restrictionId}/lift", "GET /api/v1/media/{fileId}", "GET /api/v1/notifications/unread-count",
		"POST /api/v1/conversations/{conversationId}/attachments", "GET /api/v1/attachments/{attachmentId}", "GET /api/v1/admin/message-reports/{reportId}/attachments/{attachmentId}"} {
		if !seen[want] {
			t.Errorf("Routes() lacks %q", want)
		}
	}
	// A body on DELETE can be dropped on the way, so these moved to POST.
	for _, gone := range []string{"DELETE /api/v1/account", "DELETE /api/v1/admin/restrictions/{restrictionId}"} {
		if seen[gone] {
			t.Errorf("Routes() still has %q", gone)
		}
	}
}
