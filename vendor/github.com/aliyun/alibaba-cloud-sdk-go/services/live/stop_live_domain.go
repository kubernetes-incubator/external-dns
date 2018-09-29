package live

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

// StopLiveDomain invokes the live.StopLiveDomain API synchronously
// api document: https://help.aliyun.com/api/live/stoplivedomain.html
func (client *Client) StopLiveDomain(request *StopLiveDomainRequest) (response *StopLiveDomainResponse, err error) {
	response = CreateStopLiveDomainResponse()
	err = client.DoAction(request, response)
	return
}

// StopLiveDomainWithChan invokes the live.StopLiveDomain API asynchronously
// api document: https://help.aliyun.com/api/live/stoplivedomain.html
// asynchronous document: https://help.aliyun.com/document_detail/66220.html
func (client *Client) StopLiveDomainWithChan(request *StopLiveDomainRequest) (<-chan *StopLiveDomainResponse, <-chan error) {
	responseChan := make(chan *StopLiveDomainResponse, 1)
	errChan := make(chan error, 1)
	err := client.AddAsyncTask(func() {
		defer close(responseChan)
		defer close(errChan)
		response, err := client.StopLiveDomain(request)
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

// StopLiveDomainWithCallback invokes the live.StopLiveDomain API asynchronously
// api document: https://help.aliyun.com/api/live/stoplivedomain.html
// asynchronous document: https://help.aliyun.com/document_detail/66220.html
func (client *Client) StopLiveDomainWithCallback(request *StopLiveDomainRequest, callback func(response *StopLiveDomainResponse, err error)) <-chan int {
	result := make(chan int, 1)
	err := client.AddAsyncTask(func() {
		var response *StopLiveDomainResponse
		var err error
		defer close(result)
		response, err = client.StopLiveDomain(request)
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

// StopLiveDomainRequest is the request struct for api StopLiveDomain
type StopLiveDomainRequest struct {
	*requests.RpcRequest
	SecurityToken string           `position:"Query" name:"SecurityToken"`
	DomainName    string           `position:"Query" name:"DomainName"`
	OwnerId       requests.Integer `position:"Query" name:"OwnerId"`
}

// StopLiveDomainResponse is the response struct for api StopLiveDomain
type StopLiveDomainResponse struct {
	*responses.BaseResponse
	RequestId string `json:"RequestId" xml:"RequestId"`
}

// CreateStopLiveDomainRequest creates a request to invoke StopLiveDomain API
func CreateStopLiveDomainRequest() (request *StopLiveDomainRequest) {
	request = &StopLiveDomainRequest{
		RpcRequest: &requests.RpcRequest{},
	}
	request.InitWithApiInfo("live", "2016-11-01", "StopLiveDomain", "live", "openAPI")
	return
}

// CreateStopLiveDomainResponse creates a response to parse from StopLiveDomain response
func CreateStopLiveDomainResponse() (response *StopLiveDomainResponse) {
	response = &StopLiveDomainResponse{
		BaseResponse: &responses.BaseResponse{},
	}
	return
}
