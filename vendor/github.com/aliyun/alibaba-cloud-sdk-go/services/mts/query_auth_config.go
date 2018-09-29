package mts

//Licensed under the Apache License, Version 2.0 (the "License");
//you may not use this file except in compliance with the License.
//You may obtain a copy of the License at
//
//http://www.apache.org/licenses/LICENSE-2.0
//
//Unless required by applicable law or agreed to in writing, software
//distributed under the License is distributed on an "AS IS" BASIS,
//WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//See the License for the specific language governing permissions and
//limitations under the License.
//
// Code generated by Alibaba Cloud SDK Code Generator.
// Changes may cause incorrect behavior and will be lost if the code is regenerated.

import (
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
)

// QueryAuthConfig invokes the mts.QueryAuthConfig API synchronously
// api document: https://help.aliyun.com/api/mts/queryauthconfig.html
func (client *Client) QueryAuthConfig(request *QueryAuthConfigRequest) (response *QueryAuthConfigResponse, err error) {
	response = CreateQueryAuthConfigResponse()
	err = client.DoAction(request, response)
	return
}

// QueryAuthConfigWithChan invokes the mts.QueryAuthConfig API asynchronously
// api document: https://help.aliyun.com/api/mts/queryauthconfig.html
// asynchronous document: https://help.aliyun.com/document_detail/66220.html
func (client *Client) QueryAuthConfigWithChan(request *QueryAuthConfigRequest) (<-chan *QueryAuthConfigResponse, <-chan error) {
	responseChan := make(chan *QueryAuthConfigResponse, 1)
	errChan := make(chan error, 1)
	err := client.AddAsyncTask(func() {
		defer close(responseChan)
		defer close(errChan)
		response, err := client.QueryAuthConfig(request)
		if err != nil {
			errChan <- err
		} else {
			responseChan <- response
		}
	})
	if err != nil {
		errChan <- err
		close(responseChan)
		close(errChan)
	}
	return responseChan, errChan
}

// QueryAuthConfigWithCallback invokes the mts.QueryAuthConfig API asynchronously
// api document: https://help.aliyun.com/api/mts/queryauthconfig.html
// asynchronous document: https://help.aliyun.com/document_detail/66220.html
func (client *Client) QueryAuthConfigWithCallback(request *QueryAuthConfigRequest, callback func(response *QueryAuthConfigResponse, err error)) <-chan int {
	result := make(chan int, 1)
	err := client.AddAsyncTask(func() {
		var response *QueryAuthConfigResponse
		var err error
		defer close(result)
		response, err = client.QueryAuthConfig(request)
		callback(response, err)
		result <- 1
	})
	if err != nil {
		defer close(result)
		callback(nil, err)
		result <- 0
	}
	return result
}

// QueryAuthConfigRequest is the request struct for api QueryAuthConfig
type QueryAuthConfigRequest struct {
	*requests.RpcRequest
	ResourceOwnerId      string `position:"Query" name:"ResourceOwnerId"`
	ResourceOwnerAccount string `position:"Query" name:"ResourceOwnerAccount"`
	OwnerAccount         string `position:"Query" name:"OwnerAccount"`
	OwnerId              string `position:"Query" name:"OwnerId"`
}

// QueryAuthConfigResponse is the response struct for api QueryAuthConfig
type QueryAuthConfigResponse struct {
	*responses.BaseResponse
	RequestId string `json:"RequestId" xml:"RequestId"`
	Key1      string `json:"Key1" xml:"Key1"`
	Key2      string `json:"Key2" xml:"Key2"`
}

// CreateQueryAuthConfigRequest creates a request to invoke QueryAuthConfig API
func CreateQueryAuthConfigRequest() (request *QueryAuthConfigRequest) {
	request = &QueryAuthConfigRequest{
		RpcRequest: &requests.RpcRequest{},
	}
	request.InitWithApiInfo("Mts", "2014-06-18", "QueryAuthConfig", "mts", "openAPI")
	return
}

// CreateQueryAuthConfigResponse creates a response to parse from QueryAuthConfig response
func CreateQueryAuthConfigResponse() (response *QueryAuthConfigResponse) {
	response = &QueryAuthConfigResponse{
		BaseResponse: &responses.BaseResponse{},
	}
	return
}
