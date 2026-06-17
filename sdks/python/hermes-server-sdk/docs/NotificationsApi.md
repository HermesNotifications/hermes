# hermes_server_sdk.NotificationsApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**get_notification**](NotificationsApi.md#get_notification) | **GET** /v1/notifications/{id} | Get notification status and events
[**list_notifications**](NotificationsApi.md#list_notifications) | **GET** /v1/notifications | List recent notifications
[**send_notification**](NotificationsApi.md#send_notification) | **POST** /v1/send | Send a notification


# **get_notification**
> NotificationStatusOutputBody get_notification(id)

Get notification status and events

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.notification_status_output_body import NotificationStatusOutputBody
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
    api_instance = hermes_server_sdk.NotificationsApi(api_client)
    id = 'id_example' # str | Notification ID

    try:
        # Get notification status and events
        api_response = api_instance.get_notification(id)
        print("The response of NotificationsApi->get_notification:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling NotificationsApi->get_notification: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **id** | **str**| Notification ID | 

### Return type

[**NotificationStatusOutputBody**](NotificationStatusOutputBody.md)

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

# **list_notifications**
> List[NotificationItem] list_notifications(limit=limit)

List recent notifications

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.notification_item import NotificationItem
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
    api_instance = hermes_server_sdk.NotificationsApi(api_client)
    limit = 56 # int | Max results (default 50) (optional)

    try:
        # List recent notifications
        api_response = api_instance.list_notifications(limit=limit)
        print("The response of NotificationsApi->list_notifications:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling NotificationsApi->list_notifications: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **limit** | **int**| Max results (default 50) | [optional] 

### Return type

[**List[NotificationItem]**](NotificationItem.md)

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

# **send_notification**
> SendOutputBody send_notification(send_input_body, x_idempotency_key=x_idempotency_key)

Send a notification

### Example


```python
import hermes_server_sdk
from hermes_server_sdk.models.send_input_body import SendInputBody
from hermes_server_sdk.models.send_output_body import SendOutputBody
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
    api_instance = hermes_server_sdk.NotificationsApi(api_client)
    send_input_body = hermes_server_sdk.SendInputBody() # SendInputBody | 
    x_idempotency_key = 'x_idempotency_key_example' # str | Idempotency key for deduplication (optional)

    try:
        # Send a notification
        api_response = api_instance.send_notification(send_input_body, x_idempotency_key=x_idempotency_key)
        print("The response of NotificationsApi->send_notification:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling NotificationsApi->send_notification: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **send_input_body** | [**SendInputBody**](SendInputBody.md)|  | 
 **x_idempotency_key** | **str**| Idempotency key for deduplication | [optional] 

### Return type

[**SendOutputBody**](SendOutputBody.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json, application/problem+json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**202** | Accepted |  -  |
**0** | Error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

