package rds

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

// DescribeSQLDiagnosis invokes the rds.DescribeSQLDiagnosis API synchronously
// api document: https://help.aliyun.com/api/rds/describesqldiagnosis.html
func (client *Client) DescribeSQLDiagnosis(request *DescribeSQLDiagnosisRequest) (response *DescribeSQLDiagnosisResponse, err error) {
	response = CreateDescribeSQLDiagnosisResponse()
	err = client.DoAction(request, response)
	return
}

// DescribeSQLDiagnosisWithChan invokes the rds.DescribeSQLDiagnosis API asynchronously
// api document: https://help.aliyun.com/api/rds/describesqldiagnosis.html
// asynchronous document: https://help.aliyun.com/document_detail/66220.html
func (client *Client) DescribeSQLDiagnosisWithChan(request *DescribeSQLDiagnosisRequest) (<-chan *DescribeSQLDiagnosisResponse, <-chan error) {
	responseChan := make(chan *DescribeSQLDiagnosisResponse, 1)
	errChan := make(chan error, 1)
	err := client.AddAsyncTask(func() {
		defer close(responseChan)
		defer close(errChan)
		response, err := client.DescribeSQLDiagnosis(request)
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

// DescribeSQLDiagnosisWithCallback invokes the rds.DescribeSQLDiagnosis API asynchronously
// api document: https://help.aliyun.com/api/rds/describesqldiagnosis.html
// asynchronous document: https://help.aliyun.com/document_detail/66220.html
func (client *Client) DescribeSQLDiagnosisWithCallback(request *DescribeSQLDiagnosisRequest, callback func(response *DescribeSQLDiagnosisResponse, err error)) <-chan int {
	result := make(chan int, 1)
	err := client.AddAsyncTask(func() {
		var response *DescribeSQLDiagnosisResponse
		var err error
		defer close(result)
		response, err = client.DescribeSQLDiagnosis(request)
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

// DescribeSQLDiagnosisRequest is the request struct for api DescribeSQLDiagnosis
type DescribeSQLDiagnosisRequest struct {
	*requests.RpcRequest
	DBInstanceId string `position:"Query" name:"DBInstanceId"`
	SQLDiagId    string `position:"Query" name:"SQLDiagId"`
}

// DescribeSQLDiagnosisResponse is the response struct for api DescribeSQLDiagnosis
type DescribeSQLDiagnosisResponse struct {
	*responses.BaseResponse
	RequestId string   `json:"RequestId" xml:"RequestId"`
	SQLList   []string `json:"SQLList" xml:"SQLList"`
}

// CreateDescribeSQLDiagnosisRequest creates a request to invoke DescribeSQLDiagnosis API
func CreateDescribeSQLDiagnosisRequest() (request *DescribeSQLDiagnosisRequest) {
	request = &DescribeSQLDiagnosisRequest{
		RpcRequest: &requests.RpcRequest{},
	}
	request.InitWithApiInfo("Rds", "2014-08-15", "DescribeSQLDiagnosis", "rds", "openAPI")
	return
}

// CreateDescribeSQLDiagnosisResponse creates a response to parse from DescribeSQLDiagnosis response
func CreateDescribeSQLDiagnosisResponse() (response *DescribeSQLDiagnosisResponse) {
	response = &DescribeSQLDiagnosisResponse{
		BaseResponse: &responses.BaseResponse{},
	}
	return
}
