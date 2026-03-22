# hermes_server_sdk.GroupsApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_group**](GroupsApi.md#create_group) | **POST** /v1/groups | Create a notification group
[**list_groups**](GroupsApi.md#list_groups) | **GET** /v1/groups | List notification groups
[**update_group**](GroupsApi.md#update_group) | **PUT** /v1/groups/{id} | Update a notification group


# **create_group**
> NotificationGroup create_group(create_group_input_body)

Create a notification group

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.create_group_input_body import CreateGroupInputBody
from hermes_server_sdk.models.notification_group import NotificationGroup
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
    api_instance = hermes_server_sdk.GroupsApi(api_client)
    create_group_input_body = hermes_server_sdk.CreateGroupInputBody() # CreateGroupInputBody | 

    try:
        # Create a notification group
        api_response = api_instance.create_group(create_group_input_body)
        print("The response of GroupsApi->create_group:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling GroupsApi->create_group: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **create_group_input_body** | [**CreateGroupInputBody**](CreateGroupInputBody.md)|  | 

### Return type

[**NotificationGroup**](NotificationGroup.md)

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

# **list_groups**
> List[NotificationGroup] list_groups()

List notification groups

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.notification_group import NotificationGroup
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
    api_instance = hermes_server_sdk.GroupsApi(api_client)

    try:
        # List notification groups
        api_response = api_instance.list_groups()
        print("The response of GroupsApi->list_groups:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling GroupsApi->list_groups: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**List[NotificationGroup]**](NotificationGroup.md)

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

# **update_group**
> NotificationGroup update_group(id, update_group_input_body)

Update a notification group

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.notification_group import NotificationGroup
from hermes_server_sdk.models.update_group_input_body import UpdateGroupInputBody
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
    api_instance = hermes_server_sdk.GroupsApi(api_client)
    id = 'id_example' # str | Group ID
    update_group_input_body = hermes_server_sdk.UpdateGroupInputBody() # UpdateGroupInputBody | 

    try:
        # Update a notification group
        api_response = api_instance.update_group(id, update_group_input_body)
        print("The response of GroupsApi->update_group:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling GroupsApi->update_group: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Group ID | 
 **update_group_input_body** | [**UpdateGroupInputBody**](UpdateGroupInputBody.md)|  | 

### Return type

[**NotificationGroup**](NotificationGroup.md)

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

