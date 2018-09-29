// Code generated by private/model/cli/gen-api/main.go. DO NOT EDIT.

// Package pinpointiface provides an interface to enable mocking the Amazon Pinpoint service client
// for testing your code.
//
// It is important to note that this interface will have breaking changes
// when the service model is updated and adds new API operations, paginators,
// and waiters.
package pinpointiface

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/pinpoint"
)

// PinpointAPI provides an interface to enable mocking the
// pinpoint.Pinpoint service client's API operation,
// paginators, and waiters. This make unit testing your code that calls out
// to the SDK's service client's calls easier.
//
// The best way to use this interface is so the SDK's service client's calls
// can be stubbed out for unit testing your code with the SDK without needing
// to inject custom request handlers into the SDK's request pipeline.
//
//    // myFunc uses an SDK service client to make a request to
//    // Amazon Pinpoint.
//    func myFunc(svc pinpointiface.PinpointAPI) bool {
//        // Make svc.CreateApp request
//    }
//
//    func main() {
//        sess := session.New()
//        svc := pinpoint.New(sess)
//
//        myFunc(svc)
//    }
//
// In your _test.go file:
//
//    // Define a mock struct to be used in your unit tests of myFunc.
//    type mockPinpointClient struct {
//        pinpointiface.PinpointAPI
//    }
//    func (m *mockPinpointClient) CreateApp(input *pinpoint.CreateAppInput) (*pinpoint.CreateAppOutput, error) {
//        // mock response/functionality
//    }
//
//    func TestMyFunc(t *testing.T) {
//        // Setup Test
//        mockSvc := &mockPinpointClient{}
//
//        myfunc(mockSvc)
//
//        // Verify myFunc's functionality
//    }
//
// It is important to note that this interface will have breaking changes
// when the service model is updated and adds new API operations, paginators,
// and waiters. Its suggested to use the pattern above for testing, or using
// tooling to generate mocks to satisfy the interfaces.
type PinpointAPI interface {
	CreateApp(*pinpoint.CreateAppInput) (*pinpoint.CreateAppOutput, error)
	CreateAppWithContext(aws.Context, *pinpoint.CreateAppInput, ...request.Option) (*pinpoint.CreateAppOutput, error)
	CreateAppRequest(*pinpoint.CreateAppInput) (*request.Request, *pinpoint.CreateAppOutput)

	CreateCampaign(*pinpoint.CreateCampaignInput) (*pinpoint.CreateCampaignOutput, error)
	CreateCampaignWithContext(aws.Context, *pinpoint.CreateCampaignInput, ...request.Option) (*pinpoint.CreateCampaignOutput, error)
	CreateCampaignRequest(*pinpoint.CreateCampaignInput) (*request.Request, *pinpoint.CreateCampaignOutput)

	CreateExportJob(*pinpoint.CreateExportJobInput) (*pinpoint.CreateExportJobOutput, error)
	CreateExportJobWithContext(aws.Context, *pinpoint.CreateExportJobInput, ...request.Option) (*pinpoint.CreateExportJobOutput, error)
	CreateExportJobRequest(*pinpoint.CreateExportJobInput) (*request.Request, *pinpoint.CreateExportJobOutput)

	CreateImportJob(*pinpoint.CreateImportJobInput) (*pinpoint.CreateImportJobOutput, error)
	CreateImportJobWithContext(aws.Context, *pinpoint.CreateImportJobInput, ...request.Option) (*pinpoint.CreateImportJobOutput, error)
	CreateImportJobRequest(*pinpoint.CreateImportJobInput) (*request.Request, *pinpoint.CreateImportJobOutput)

	CreateSegment(*pinpoint.CreateSegmentInput) (*pinpoint.CreateSegmentOutput, error)
	CreateSegmentWithContext(aws.Context, *pinpoint.CreateSegmentInput, ...request.Option) (*pinpoint.CreateSegmentOutput, error)
	CreateSegmentRequest(*pinpoint.CreateSegmentInput) (*request.Request, *pinpoint.CreateSegmentOutput)

	DeleteAdmChannel(*pinpoint.DeleteAdmChannelInput) (*pinpoint.DeleteAdmChannelOutput, error)
	DeleteAdmChannelWithContext(aws.Context, *pinpoint.DeleteAdmChannelInput, ...request.Option) (*pinpoint.DeleteAdmChannelOutput, error)
	DeleteAdmChannelRequest(*pinpoint.DeleteAdmChannelInput) (*request.Request, *pinpoint.DeleteAdmChannelOutput)

	DeleteApnsChannel(*pinpoint.DeleteApnsChannelInput) (*pinpoint.DeleteApnsChannelOutput, error)
	DeleteApnsChannelWithContext(aws.Context, *pinpoint.DeleteApnsChannelInput, ...request.Option) (*pinpoint.DeleteApnsChannelOutput, error)
	DeleteApnsChannelRequest(*pinpoint.DeleteApnsChannelInput) (*request.Request, *pinpoint.DeleteApnsChannelOutput)

	DeleteApnsSandboxChannel(*pinpoint.DeleteApnsSandboxChannelInput) (*pinpoint.DeleteApnsSandboxChannelOutput, error)
	DeleteApnsSandboxChannelWithContext(aws.Context, *pinpoint.DeleteApnsSandboxChannelInput, ...request.Option) (*pinpoint.DeleteApnsSandboxChannelOutput, error)
	DeleteApnsSandboxChannelRequest(*pinpoint.DeleteApnsSandboxChannelInput) (*request.Request, *pinpoint.DeleteApnsSandboxChannelOutput)

	DeleteApnsVoipChannel(*pinpoint.DeleteApnsVoipChannelInput) (*pinpoint.DeleteApnsVoipChannelOutput, error)
	DeleteApnsVoipChannelWithContext(aws.Context, *pinpoint.DeleteApnsVoipChannelInput, ...request.Option) (*pinpoint.DeleteApnsVoipChannelOutput, error)
	DeleteApnsVoipChannelRequest(*pinpoint.DeleteApnsVoipChannelInput) (*request.Request, *pinpoint.DeleteApnsVoipChannelOutput)

	DeleteApnsVoipSandboxChannel(*pinpoint.DeleteApnsVoipSandboxChannelInput) (*pinpoint.DeleteApnsVoipSandboxChannelOutput, error)
	DeleteApnsVoipSandboxChannelWithContext(aws.Context, *pinpoint.DeleteApnsVoipSandboxChannelInput, ...request.Option) (*pinpoint.DeleteApnsVoipSandboxChannelOutput, error)
	DeleteApnsVoipSandboxChannelRequest(*pinpoint.DeleteApnsVoipSandboxChannelInput) (*request.Request, *pinpoint.DeleteApnsVoipSandboxChannelOutput)

	DeleteApp(*pinpoint.DeleteAppInput) (*pinpoint.DeleteAppOutput, error)
	DeleteAppWithContext(aws.Context, *pinpoint.DeleteAppInput, ...request.Option) (*pinpoint.DeleteAppOutput, error)
	DeleteAppRequest(*pinpoint.DeleteAppInput) (*request.Request, *pinpoint.DeleteAppOutput)

	DeleteBaiduChannel(*pinpoint.DeleteBaiduChannelInput) (*pinpoint.DeleteBaiduChannelOutput, error)
	DeleteBaiduChannelWithContext(aws.Context, *pinpoint.DeleteBaiduChannelInput, ...request.Option) (*pinpoint.DeleteBaiduChannelOutput, error)
	DeleteBaiduChannelRequest(*pinpoint.DeleteBaiduChannelInput) (*request.Request, *pinpoint.DeleteBaiduChannelOutput)

	DeleteCampaign(*pinpoint.DeleteCampaignInput) (*pinpoint.DeleteCampaignOutput, error)
	DeleteCampaignWithContext(aws.Context, *pinpoint.DeleteCampaignInput, ...request.Option) (*pinpoint.DeleteCampaignOutput, error)
	DeleteCampaignRequest(*pinpoint.DeleteCampaignInput) (*request.Request, *pinpoint.DeleteCampaignOutput)

	DeleteEmailChannel(*pinpoint.DeleteEmailChannelInput) (*pinpoint.DeleteEmailChannelOutput, error)
	DeleteEmailChannelWithContext(aws.Context, *pinpoint.DeleteEmailChannelInput, ...request.Option) (*pinpoint.DeleteEmailChannelOutput, error)
	DeleteEmailChannelRequest(*pinpoint.DeleteEmailChannelInput) (*request.Request, *pinpoint.DeleteEmailChannelOutput)

	DeleteEndpoint(*pinpoint.DeleteEndpointInput) (*pinpoint.DeleteEndpointOutput, error)
	DeleteEndpointWithContext(aws.Context, *pinpoint.DeleteEndpointInput, ...request.Option) (*pinpoint.DeleteEndpointOutput, error)
	DeleteEndpointRequest(*pinpoint.DeleteEndpointInput) (*request.Request, *pinpoint.DeleteEndpointOutput)

	DeleteEventStream(*pinpoint.DeleteEventStreamInput) (*pinpoint.DeleteEventStreamOutput, error)
	DeleteEventStreamWithContext(aws.Context, *pinpoint.DeleteEventStreamInput, ...request.Option) (*pinpoint.DeleteEventStreamOutput, error)
	DeleteEventStreamRequest(*pinpoint.DeleteEventStreamInput) (*request.Request, *pinpoint.DeleteEventStreamOutput)

	DeleteGcmChannel(*pinpoint.DeleteGcmChannelInput) (*pinpoint.DeleteGcmChannelOutput, error)
	DeleteGcmChannelWithContext(aws.Context, *pinpoint.DeleteGcmChannelInput, ...request.Option) (*pinpoint.DeleteGcmChannelOutput, error)
	DeleteGcmChannelRequest(*pinpoint.DeleteGcmChannelInput) (*request.Request, *pinpoint.DeleteGcmChannelOutput)

	DeleteSegment(*pinpoint.DeleteSegmentInput) (*pinpoint.DeleteSegmentOutput, error)
	DeleteSegmentWithContext(aws.Context, *pinpoint.DeleteSegmentInput, ...request.Option) (*pinpoint.DeleteSegmentOutput, error)
	DeleteSegmentRequest(*pinpoint.DeleteSegmentInput) (*request.Request, *pinpoint.DeleteSegmentOutput)

	DeleteSmsChannel(*pinpoint.DeleteSmsChannelInput) (*pinpoint.DeleteSmsChannelOutput, error)
	DeleteSmsChannelWithContext(aws.Context, *pinpoint.DeleteSmsChannelInput, ...request.Option) (*pinpoint.DeleteSmsChannelOutput, error)
	DeleteSmsChannelRequest(*pinpoint.DeleteSmsChannelInput) (*request.Request, *pinpoint.DeleteSmsChannelOutput)

	GetAdmChannel(*pinpoint.GetAdmChannelInput) (*pinpoint.GetAdmChannelOutput, error)
	GetAdmChannelWithContext(aws.Context, *pinpoint.GetAdmChannelInput, ...request.Option) (*pinpoint.GetAdmChannelOutput, error)
	GetAdmChannelRequest(*pinpoint.GetAdmChannelInput) (*request.Request, *pinpoint.GetAdmChannelOutput)

	GetApnsChannel(*pinpoint.GetApnsChannelInput) (*pinpoint.GetApnsChannelOutput, error)
	GetApnsChannelWithContext(aws.Context, *pinpoint.GetApnsChannelInput, ...request.Option) (*pinpoint.GetApnsChannelOutput, error)
	GetApnsChannelRequest(*pinpoint.GetApnsChannelInput) (*request.Request, *pinpoint.GetApnsChannelOutput)

	GetApnsSandboxChannel(*pinpoint.GetApnsSandboxChannelInput) (*pinpoint.GetApnsSandboxChannelOutput, error)
	GetApnsSandboxChannelWithContext(aws.Context, *pinpoint.GetApnsSandboxChannelInput, ...request.Option) (*pinpoint.GetApnsSandboxChannelOutput, error)
	GetApnsSandboxChannelRequest(*pinpoint.GetApnsSandboxChannelInput) (*request.Request, *pinpoint.GetApnsSandboxChannelOutput)

	GetApnsVoipChannel(*pinpoint.GetApnsVoipChannelInput) (*pinpoint.GetApnsVoipChannelOutput, error)
	GetApnsVoipChannelWithContext(aws.Context, *pinpoint.GetApnsVoipChannelInput, ...request.Option) (*pinpoint.GetApnsVoipChannelOutput, error)
	GetApnsVoipChannelRequest(*pinpoint.GetApnsVoipChannelInput) (*request.Request, *pinpoint.GetApnsVoipChannelOutput)

	GetApnsVoipSandboxChannel(*pinpoint.GetApnsVoipSandboxChannelInput) (*pinpoint.GetApnsVoipSandboxChannelOutput, error)
	GetApnsVoipSandboxChannelWithContext(aws.Context, *pinpoint.GetApnsVoipSandboxChannelInput, ...request.Option) (*pinpoint.GetApnsVoipSandboxChannelOutput, error)
	GetApnsVoipSandboxChannelRequest(*pinpoint.GetApnsVoipSandboxChannelInput) (*request.Request, *pinpoint.GetApnsVoipSandboxChannelOutput)

	GetApp(*pinpoint.GetAppInput) (*pinpoint.GetAppOutput, error)
	GetAppWithContext(aws.Context, *pinpoint.GetAppInput, ...request.Option) (*pinpoint.GetAppOutput, error)
	GetAppRequest(*pinpoint.GetAppInput) (*request.Request, *pinpoint.GetAppOutput)

	GetApplicationSettings(*pinpoint.GetApplicationSettingsInput) (*pinpoint.GetApplicationSettingsOutput, error)
	GetApplicationSettingsWithContext(aws.Context, *pinpoint.GetApplicationSettingsInput, ...request.Option) (*pinpoint.GetApplicationSettingsOutput, error)
	GetApplicationSettingsRequest(*pinpoint.GetApplicationSettingsInput) (*request.Request, *pinpoint.GetApplicationSettingsOutput)

	GetApps(*pinpoint.GetAppsInput) (*pinpoint.GetAppsOutput, error)
	GetAppsWithContext(aws.Context, *pinpoint.GetAppsInput, ...request.Option) (*pinpoint.GetAppsOutput, error)
	GetAppsRequest(*pinpoint.GetAppsInput) (*request.Request, *pinpoint.GetAppsOutput)

	GetBaiduChannel(*pinpoint.GetBaiduChannelInput) (*pinpoint.GetBaiduChannelOutput, error)
	GetBaiduChannelWithContext(aws.Context, *pinpoint.GetBaiduChannelInput, ...request.Option) (*pinpoint.GetBaiduChannelOutput, error)
	GetBaiduChannelRequest(*pinpoint.GetBaiduChannelInput) (*request.Request, *pinpoint.GetBaiduChannelOutput)

	GetCampaign(*pinpoint.GetCampaignInput) (*pinpoint.GetCampaignOutput, error)
	GetCampaignWithContext(aws.Context, *pinpoint.GetCampaignInput, ...request.Option) (*pinpoint.GetCampaignOutput, error)
	GetCampaignRequest(*pinpoint.GetCampaignInput) (*request.Request, *pinpoint.GetCampaignOutput)

	GetCampaignActivities(*pinpoint.GetCampaignActivitiesInput) (*pinpoint.GetCampaignActivitiesOutput, error)
	GetCampaignActivitiesWithContext(aws.Context, *pinpoint.GetCampaignActivitiesInput, ...request.Option) (*pinpoint.GetCampaignActivitiesOutput, error)
	GetCampaignActivitiesRequest(*pinpoint.GetCampaignActivitiesInput) (*request.Request, *pinpoint.GetCampaignActivitiesOutput)

	GetCampaignVersion(*pinpoint.GetCampaignVersionInput) (*pinpoint.GetCampaignVersionOutput, error)
	GetCampaignVersionWithContext(aws.Context, *pinpoint.GetCampaignVersionInput, ...request.Option) (*pinpoint.GetCampaignVersionOutput, error)
	GetCampaignVersionRequest(*pinpoint.GetCampaignVersionInput) (*request.Request, *pinpoint.GetCampaignVersionOutput)

	GetCampaignVersions(*pinpoint.GetCampaignVersionsInput) (*pinpoint.GetCampaignVersionsOutput, error)
	GetCampaignVersionsWithContext(aws.Context, *pinpoint.GetCampaignVersionsInput, ...request.Option) (*pinpoint.GetCampaignVersionsOutput, error)
	GetCampaignVersionsRequest(*pinpoint.GetCampaignVersionsInput) (*request.Request, *pinpoint.GetCampaignVersionsOutput)

	GetCampaigns(*pinpoint.GetCampaignsInput) (*pinpoint.GetCampaignsOutput, error)
	GetCampaignsWithContext(aws.Context, *pinpoint.GetCampaignsInput, ...request.Option) (*pinpoint.GetCampaignsOutput, error)
	GetCampaignsRequest(*pinpoint.GetCampaignsInput) (*request.Request, *pinpoint.GetCampaignsOutput)

	GetEmailChannel(*pinpoint.GetEmailChannelInput) (*pinpoint.GetEmailChannelOutput, error)
	GetEmailChannelWithContext(aws.Context, *pinpoint.GetEmailChannelInput, ...request.Option) (*pinpoint.GetEmailChannelOutput, error)
	GetEmailChannelRequest(*pinpoint.GetEmailChannelInput) (*request.Request, *pinpoint.GetEmailChannelOutput)

	GetEndpoint(*pinpoint.GetEndpointInput) (*pinpoint.GetEndpointOutput, error)
	GetEndpointWithContext(aws.Context, *pinpoint.GetEndpointInput, ...request.Option) (*pinpoint.GetEndpointOutput, error)
	GetEndpointRequest(*pinpoint.GetEndpointInput) (*request.Request, *pinpoint.GetEndpointOutput)

	GetEventStream(*pinpoint.GetEventStreamInput) (*pinpoint.GetEventStreamOutput, error)
	GetEventStreamWithContext(aws.Context, *pinpoint.GetEventStreamInput, ...request.Option) (*pinpoint.GetEventStreamOutput, error)
	GetEventStreamRequest(*pinpoint.GetEventStreamInput) (*request.Request, *pinpoint.GetEventStreamOutput)

	GetExportJob(*pinpoint.GetExportJobInput) (*pinpoint.GetExportJobOutput, error)
	GetExportJobWithContext(aws.Context, *pinpoint.GetExportJobInput, ...request.Option) (*pinpoint.GetExportJobOutput, error)
	GetExportJobRequest(*pinpoint.GetExportJobInput) (*request.Request, *pinpoint.GetExportJobOutput)

	GetExportJobs(*pinpoint.GetExportJobsInput) (*pinpoint.GetExportJobsOutput, error)
	GetExportJobsWithContext(aws.Context, *pinpoint.GetExportJobsInput, ...request.Option) (*pinpoint.GetExportJobsOutput, error)
	GetExportJobsRequest(*pinpoint.GetExportJobsInput) (*request.Request, *pinpoint.GetExportJobsOutput)

	GetGcmChannel(*pinpoint.GetGcmChannelInput) (*pinpoint.GetGcmChannelOutput, error)
	GetGcmChannelWithContext(aws.Context, *pinpoint.GetGcmChannelInput, ...request.Option) (*pinpoint.GetGcmChannelOutput, error)
	GetGcmChannelRequest(*pinpoint.GetGcmChannelInput) (*request.Request, *pinpoint.GetGcmChannelOutput)

	GetImportJob(*pinpoint.GetImportJobInput) (*pinpoint.GetImportJobOutput, error)
	GetImportJobWithContext(aws.Context, *pinpoint.GetImportJobInput, ...request.Option) (*pinpoint.GetImportJobOutput, error)
	GetImportJobRequest(*pinpoint.GetImportJobInput) (*request.Request, *pinpoint.GetImportJobOutput)

	GetImportJobs(*pinpoint.GetImportJobsInput) (*pinpoint.GetImportJobsOutput, error)
	GetImportJobsWithContext(aws.Context, *pinpoint.GetImportJobsInput, ...request.Option) (*pinpoint.GetImportJobsOutput, error)
	GetImportJobsRequest(*pinpoint.GetImportJobsInput) (*request.Request, *pinpoint.GetImportJobsOutput)

	GetSegment(*pinpoint.GetSegmentInput) (*pinpoint.GetSegmentOutput, error)
	GetSegmentWithContext(aws.Context, *pinpoint.GetSegmentInput, ...request.Option) (*pinpoint.GetSegmentOutput, error)
	GetSegmentRequest(*pinpoint.GetSegmentInput) (*request.Request, *pinpoint.GetSegmentOutput)

	GetSegmentExportJobs(*pinpoint.GetSegmentExportJobsInput) (*pinpoint.GetSegmentExportJobsOutput, error)
	GetSegmentExportJobsWithContext(aws.Context, *pinpoint.GetSegmentExportJobsInput, ...request.Option) (*pinpoint.GetSegmentExportJobsOutput, error)
	GetSegmentExportJobsRequest(*pinpoint.GetSegmentExportJobsInput) (*request.Request, *pinpoint.GetSegmentExportJobsOutput)

	GetSegmentImportJobs(*pinpoint.GetSegmentImportJobsInput) (*pinpoint.GetSegmentImportJobsOutput, error)
	GetSegmentImportJobsWithContext(aws.Context, *pinpoint.GetSegmentImportJobsInput, ...request.Option) (*pinpoint.GetSegmentImportJobsOutput, error)
	GetSegmentImportJobsRequest(*pinpoint.GetSegmentImportJobsInput) (*request.Request, *pinpoint.GetSegmentImportJobsOutput)

	GetSegmentVersion(*pinpoint.GetSegmentVersionInput) (*pinpoint.GetSegmentVersionOutput, error)
	GetSegmentVersionWithContext(aws.Context, *pinpoint.GetSegmentVersionInput, ...request.Option) (*pinpoint.GetSegmentVersionOutput, error)
	GetSegmentVersionRequest(*pinpoint.GetSegmentVersionInput) (*request.Request, *pinpoint.GetSegmentVersionOutput)

	GetSegmentVersions(*pinpoint.GetSegmentVersionsInput) (*pinpoint.GetSegmentVersionsOutput, error)
	GetSegmentVersionsWithContext(aws.Context, *pinpoint.GetSegmentVersionsInput, ...request.Option) (*pinpoint.GetSegmentVersionsOutput, error)
	GetSegmentVersionsRequest(*pinpoint.GetSegmentVersionsInput) (*request.Request, *pinpoint.GetSegmentVersionsOutput)

	GetSegments(*pinpoint.GetSegmentsInput) (*pinpoint.GetSegmentsOutput, error)
	GetSegmentsWithContext(aws.Context, *pinpoint.GetSegmentsInput, ...request.Option) (*pinpoint.GetSegmentsOutput, error)
	GetSegmentsRequest(*pinpoint.GetSegmentsInput) (*request.Request, *pinpoint.GetSegmentsOutput)

	GetSmsChannel(*pinpoint.GetSmsChannelInput) (*pinpoint.GetSmsChannelOutput, error)
	GetSmsChannelWithContext(aws.Context, *pinpoint.GetSmsChannelInput, ...request.Option) (*pinpoint.GetSmsChannelOutput, error)
	GetSmsChannelRequest(*pinpoint.GetSmsChannelInput) (*request.Request, *pinpoint.GetSmsChannelOutput)

	PutEventStream(*pinpoint.PutEventStreamInput) (*pinpoint.PutEventStreamOutput, error)
	PutEventStreamWithContext(aws.Context, *pinpoint.PutEventStreamInput, ...request.Option) (*pinpoint.PutEventStreamOutput, error)
	PutEventStreamRequest(*pinpoint.PutEventStreamInput) (*request.Request, *pinpoint.PutEventStreamOutput)

	SendMessages(*pinpoint.SendMessagesInput) (*pinpoint.SendMessagesOutput, error)
	SendMessagesWithContext(aws.Context, *pinpoint.SendMessagesInput, ...request.Option) (*pinpoint.SendMessagesOutput, error)
	SendMessagesRequest(*pinpoint.SendMessagesInput) (*request.Request, *pinpoint.SendMessagesOutput)

	SendUsersMessages(*pinpoint.SendUsersMessagesInput) (*pinpoint.SendUsersMessagesOutput, error)
	SendUsersMessagesWithContext(aws.Context, *pinpoint.SendUsersMessagesInput, ...request.Option) (*pinpoint.SendUsersMessagesOutput, error)
	SendUsersMessagesRequest(*pinpoint.SendUsersMessagesInput) (*request.Request, *pinpoint.SendUsersMessagesOutput)

	UpdateAdmChannel(*pinpoint.UpdateAdmChannelInput) (*pinpoint.UpdateAdmChannelOutput, error)
	UpdateAdmChannelWithContext(aws.Context, *pinpoint.UpdateAdmChannelInput, ...request.Option) (*pinpoint.UpdateAdmChannelOutput, error)
	UpdateAdmChannelRequest(*pinpoint.UpdateAdmChannelInput) (*request.Request, *pinpoint.UpdateAdmChannelOutput)

	UpdateApnsChannel(*pinpoint.UpdateApnsChannelInput) (*pinpoint.UpdateApnsChannelOutput, error)
	UpdateApnsChannelWithContext(aws.Context, *pinpoint.UpdateApnsChannelInput, ...request.Option) (*pinpoint.UpdateApnsChannelOutput, error)
	UpdateApnsChannelRequest(*pinpoint.UpdateApnsChannelInput) (*request.Request, *pinpoint.UpdateApnsChannelOutput)

	UpdateApnsSandboxChannel(*pinpoint.UpdateApnsSandboxChannelInput) (*pinpoint.UpdateApnsSandboxChannelOutput, error)
	UpdateApnsSandboxChannelWithContext(aws.Context, *pinpoint.UpdateApnsSandboxChannelInput, ...request.Option) (*pinpoint.UpdateApnsSandboxChannelOutput, error)
	UpdateApnsSandboxChannelRequest(*pinpoint.UpdateApnsSandboxChannelInput) (*request.Request, *pinpoint.UpdateApnsSandboxChannelOutput)

	UpdateApnsVoipChannel(*pinpoint.UpdateApnsVoipChannelInput) (*pinpoint.UpdateApnsVoipChannelOutput, error)
	UpdateApnsVoipChannelWithContext(aws.Context, *pinpoint.UpdateApnsVoipChannelInput, ...request.Option) (*pinpoint.UpdateApnsVoipChannelOutput, error)
	UpdateApnsVoipChannelRequest(*pinpoint.UpdateApnsVoipChannelInput) (*request.Request, *pinpoint.UpdateApnsVoipChannelOutput)

	UpdateApnsVoipSandboxChannel(*pinpoint.UpdateApnsVoipSandboxChannelInput) (*pinpoint.UpdateApnsVoipSandboxChannelOutput, error)
	UpdateApnsVoipSandboxChannelWithContext(aws.Context, *pinpoint.UpdateApnsVoipSandboxChannelInput, ...request.Option) (*pinpoint.UpdateApnsVoipSandboxChannelOutput, error)
	UpdateApnsVoipSandboxChannelRequest(*pinpoint.UpdateApnsVoipSandboxChannelInput) (*request.Request, *pinpoint.UpdateApnsVoipSandboxChannelOutput)

	UpdateApplicationSettings(*pinpoint.UpdateApplicationSettingsInput) (*pinpoint.UpdateApplicationSettingsOutput, error)
	UpdateApplicationSettingsWithContext(aws.Context, *pinpoint.UpdateApplicationSettingsInput, ...request.Option) (*pinpoint.UpdateApplicationSettingsOutput, error)
	UpdateApplicationSettingsRequest(*pinpoint.UpdateApplicationSettingsInput) (*request.Request, *pinpoint.UpdateApplicationSettingsOutput)

	UpdateBaiduChannel(*pinpoint.UpdateBaiduChannelInput) (*pinpoint.UpdateBaiduChannelOutput, error)
	UpdateBaiduChannelWithContext(aws.Context, *pinpoint.UpdateBaiduChannelInput, ...request.Option) (*pinpoint.UpdateBaiduChannelOutput, error)
	UpdateBaiduChannelRequest(*pinpoint.UpdateBaiduChannelInput) (*request.Request, *pinpoint.UpdateBaiduChannelOutput)

	UpdateCampaign(*pinpoint.UpdateCampaignInput) (*pinpoint.UpdateCampaignOutput, error)
	UpdateCampaignWithContext(aws.Context, *pinpoint.UpdateCampaignInput, ...request.Option) (*pinpoint.UpdateCampaignOutput, error)
	UpdateCampaignRequest(*pinpoint.UpdateCampaignInput) (*request.Request, *pinpoint.UpdateCampaignOutput)

	UpdateEmailChannel(*pinpoint.UpdateEmailChannelInput) (*pinpoint.UpdateEmailChannelOutput, error)
	UpdateEmailChannelWithContext(aws.Context, *pinpoint.UpdateEmailChannelInput, ...request.Option) (*pinpoint.UpdateEmailChannelOutput, error)
	UpdateEmailChannelRequest(*pinpoint.UpdateEmailChannelInput) (*request.Request, *pinpoint.UpdateEmailChannelOutput)

	UpdateEndpoint(*pinpoint.UpdateEndpointInput) (*pinpoint.UpdateEndpointOutput, error)
	UpdateEndpointWithContext(aws.Context, *pinpoint.UpdateEndpointInput, ...request.Option) (*pinpoint.UpdateEndpointOutput, error)
	UpdateEndpointRequest(*pinpoint.UpdateEndpointInput) (*request.Request, *pinpoint.UpdateEndpointOutput)

	UpdateEndpointsBatch(*pinpoint.UpdateEndpointsBatchInput) (*pinpoint.UpdateEndpointsBatchOutput, error)
	UpdateEndpointsBatchWithContext(aws.Context, *pinpoint.UpdateEndpointsBatchInput, ...request.Option) (*pinpoint.UpdateEndpointsBatchOutput, error)
	UpdateEndpointsBatchRequest(*pinpoint.UpdateEndpointsBatchInput) (*request.Request, *pinpoint.UpdateEndpointsBatchOutput)

	UpdateGcmChannel(*pinpoint.UpdateGcmChannelInput) (*pinpoint.UpdateGcmChannelOutput, error)
	UpdateGcmChannelWithContext(aws.Context, *pinpoint.UpdateGcmChannelInput, ...request.Option) (*pinpoint.UpdateGcmChannelOutput, error)
	UpdateGcmChannelRequest(*pinpoint.UpdateGcmChannelInput) (*request.Request, *pinpoint.UpdateGcmChannelOutput)

	UpdateSegment(*pinpoint.UpdateSegmentInput) (*pinpoint.UpdateSegmentOutput, error)
	UpdateSegmentWithContext(aws.Context, *pinpoint.UpdateSegmentInput, ...request.Option) (*pinpoint.UpdateSegmentOutput, error)
	UpdateSegmentRequest(*pinpoint.UpdateSegmentInput) (*request.Request, *pinpoint.UpdateSegmentOutput)

	UpdateSmsChannel(*pinpoint.UpdateSmsChannelInput) (*pinpoint.UpdateSmsChannelOutput, error)
	UpdateSmsChannelWithContext(aws.Context, *pinpoint.UpdateSmsChannelInput, ...request.Option) (*pinpoint.UpdateSmsChannelOutput, error)
	UpdateSmsChannelRequest(*pinpoint.UpdateSmsChannelInput) (*request.Request, *pinpoint.UpdateSmsChannelOutput)
}

var _ PinpointAPI = (*pinpoint.Pinpoint)(nil)
