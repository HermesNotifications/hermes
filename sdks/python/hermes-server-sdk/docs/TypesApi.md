# hermes_server_sdk.TypesApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_type**](TypesApi.md#create_type) | **POST** /v1/types | Create a notification type
[**delete_type**](TypesApi.md#delete_type) | **DELETE** /v1/types/{id} | Delete a notification type
[**list_types**](TypesApi.md#list_types) | **GET** /v1/types | List notification types
[**update_type**](TypesApi.md#update_type) | **PUT** /v1/types/{id} | Update a notification type


# **create_type**
> NotificationType create_type(create_type_input_body)

Create a notification type

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.create_type_input_body import CreateTypeInputBody
from hermes_server_sdk.models.notification_type import NotificationType
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
    api_instance = hermes_server_sdk.TypesApi(api_client)
    create_type_input_body = hermes_server_sdk.CreateTypeInputBody() # CreateTypeInputBody | 

    try:
        # Create a notification type
        api_response = api_instance.create_type(create_type_input_body)
        print("The response of TypesApi->create_type:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling TypesApi->create_type: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_type_input_body** | [**CreateTypeInputBody**](CreateTypeInputBody.md)|  | 

### Return type

[**NotificationType**](NotificationType.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**201** | Created |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_type**
> delete_type(id)

Delete a notification type

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
    api_instance = hermes_server_sdk.TypesApi(api_client)
    id = 'id_example' # str | Type ID

    try:
        # Delete a notification type
        api_instance.delete_type(id)
    except Exception as e:
        print("Exception when calling TypesApi->delete_type: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Type ID | 

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**204** | No Content |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_types**
> List[NotificationType] list_types()

List notification types

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.notification_type import NotificationType
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
    api_instance = hermes_server_sdk.TypesApi(api_client)

    try:
        # List notification types
        api_response = api_instance.list_types()
        print("The response of TypesApi->list_types:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling TypesApi->list_types: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**List[NotificationType]**](NotificationType.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | OK |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_type**
> NotificationType update_type(id, update_type_input_body)

Update a notification type

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.notification_type import NotificationType
from hermes_server_sdk.models.update_type_input_body import UpdateTypeInputBody
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
    api_instance = hermes_server_sdk.TypesApi(api_client)
    id = 'id_example' # str | Type ID
    update_type_input_body = hermes_server_sdk.UpdateTypeInputBody() # UpdateTypeInputBody | 

    try:
        # Update a notification type
        api_response = api_instance.update_type(id, update_type_input_body)
        print("The response of TypesApi->update_type:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling TypesApi->update_type: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Type ID | 
 **update_type_input_body** | [**UpdateTypeInputBody**](UpdateTypeInputBody.md)|  | 

### Return type

[**NotificationType**](NotificationType.md)

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

