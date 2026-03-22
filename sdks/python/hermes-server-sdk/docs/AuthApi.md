# hermes_server_sdk.AuthApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**exchange_token**](AuthApi.md#exchange_token) | **POST** /v1/auth/token | Exchange credentials for a user JWT token


# **exchange_token**
> TokenOutputBody exchange_token(token_input_body)

Exchange credentials for a user JWT token

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.token_input_body import TokenInputBody
from hermes_server_sdk.models.token_output_body import TokenOutputBody
from hermes_server_sdk.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = hermes_server_sdk.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with hermes_server_sdk.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = hermes_server_sdk.AuthApi(api_client)
    token_input_body = hermes_server_sdk.TokenInputBody() # TokenInputBody | 

    try:
        # Exchange credentials for a user JWT token
        api_response = api_instance.exchange_token(token_input_body)
        print("The response of AuthApi->exchange_token:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling AuthApi->exchange_token: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **token_input_body** | [**TokenInputBody**](TokenInputBody.md)|  | 

### Return type

[**TokenOutputBody**](TokenOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

