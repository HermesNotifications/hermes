# hermes_server_sdk.APIKeysApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_api_key**](APIKeysApi.md#create_api_key) | **POST** /v1/apikeys | Create a new API key
[**delete_api_key**](APIKeysApi.md#delete_api_key) | **DELETE** /v1/apikeys/{id} | Revoke an API key
[**list_api_keys**](APIKeysApi.md#list_api_keys) | **GET** /v1/apikeys | List all API keys
[**set_api_key_rate_limit**](APIKeysApi.md#set_api_key_rate_limit) | **PUT** /v1/apikeys/{id}/rate-limit | Set or clear a key&#39;s rate limit


# **create_api_key**
> ApiKeyCreatedOutputBody create_api_key(create_api_key_input_body)

Create a new API key

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.api_key_created_output_body import ApiKeyCreatedOutputBody
from hermes_server_sdk.models.create_api_key_input_body import CreateAPIKeyInputBody
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
    api_instance = hermes_server_sdk.APIKeysApi(api_client)
    create_api_key_input_body = hermes_server_sdk.CreateAPIKeyInputBody() # CreateAPIKeyInputBody | 

    try:
        # Create a new API key
        api_response = api_instance.create_api_key(create_api_key_input_body)
        print("The response of APIKeysApi->create_api_key:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling APIKeysApi->create_api_key: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_api_key_input_body** | [**CreateAPIKeyInputBody**](CreateAPIKeyInputBody.md)|  | 

### Return type

[**ApiKeyCreatedOutputBody**](ApiKeyCreatedOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**201** | Created |  -  |
**429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_api_key**
> delete_api_key(id)

Revoke an API key

### Example


```python
import hermes_server_sdk
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
    api_instance = hermes_server_sdk.APIKeysApi(api_client)
    id = 'id_example' # str | API key ID

    try:
        # Revoke an API key
        api_instance.delete_api_key(id)
    except Exception as e:
        print("Exception when calling APIKeysApi->delete_api_key: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| API key ID | 

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**204** | No Content |  -  |
**429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_api_keys**
> List[ApiKeyView] list_api_keys()

List all API keys

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.api_key_view import ApiKeyView
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
    api_instance = hermes_server_sdk.APIKeysApi(api_client)

    try:
        # List all API keys
        api_response = api_instance.list_api_keys()
        print("The response of APIKeysApi->list_api_keys:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling APIKeysApi->list_api_keys: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**List[ApiKeyView]**](ApiKeyView.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **set_api_key_rate_limit**
> ApiKeyView set_api_key_rate_limit(id, set_api_key_rate_limit_input_body)

Set or clear a key's rate limit

Replaces this key's rate limit. Omitted fields reset to the service default, so an empty body clears the override entirely. This endpoint invalidates the key's cache entry, so the new limit applies to the next bucket created for it — but a caller that is continuously active keeps its current bucket, and therefore its old limit, until it goes idle. Do not rely on this to throttle a caller mid-flood.

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.api_key_view import ApiKeyView
from hermes_server_sdk.models.set_api_key_rate_limit_input_body import SetAPIKeyRateLimitInputBody
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
    api_instance = hermes_server_sdk.APIKeysApi(api_client)
    id = 'id_example' # str | API key ID
    set_api_key_rate_limit_input_body = hermes_server_sdk.SetAPIKeyRateLimitInputBody() # SetAPIKeyRateLimitInputBody | 

    try:
        # Set or clear a key's rate limit
        api_response = api_instance.set_api_key_rate_limit(id, set_api_key_rate_limit_input_body)
        print("The response of APIKeysApi->set_api_key_rate_limit:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling APIKeysApi->set_api_key_rate_limit: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| API key ID | 
 **set_api_key_rate_limit_input_body** | [**SetAPIKeyRateLimitInputBody**](SetAPIKeyRateLimitInputBody.md)|  | 

### Return type

[**ApiKeyView**](ApiKeyView.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**429** | Too Many Requests. The caller exceeded its rate limit. Honour Retry-After; retrying sooner does not shorten the wait. A 429 from the pre-authentication per-address bound carries only Retry-After, without the RateLimit-* headers. |  * RateLimit-Limit - Sustained requests per second allowed for this credential. <br>  * RateLimit-Remaining - Requests available right now. <br>  * RateLimit-Reset - Seconds until capacity is available. <br>  * Retry-After - Whole seconds to wait before retrying. Always at least 1. <br>  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

