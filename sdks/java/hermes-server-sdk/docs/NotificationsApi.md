# NotificationsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**getNotification**](NotificationsApi.md#getNotification) | **GET** /v1/notifications/{id} | Get notification status and events |
| [**listNotifications**](NotificationsApi.md#listNotifications) | **GET** /v1/notifications | List recent notifications |
| [**sendNotification**](NotificationsApi.md#sendNotification) | **POST** /v1/send | Send a notification |


<a id="getNotification"></a>
# **getNotification**
> NotificationStatusOutputBody getNotification(id)

Get notification status and events

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.NotificationsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    NotificationsApi apiInstance = new NotificationsApi(defaultClient);
    String id = "id_example"; // String | Notification ID
    try {
      NotificationStatusOutputBody result = apiInstance.getNotification(id);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling NotificationsApi#getNotification");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **id** | **String**| Notification ID | |

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
| **200** | OK |  -  |
| **0** | Error |  -  |

<a id="listNotifications"></a>
# **listNotifications**
> List&lt;NotificationItem&gt; listNotifications(limit)

List recent notifications

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.NotificationsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    NotificationsApi apiInstance = new NotificationsApi(defaultClient);
    Long limit = 56L; // Long | Max results (default 50)
    try {
      List<NotificationItem> result = apiInstance.listNotifications(limit);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling NotificationsApi#listNotifications");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **limit** | **Long**| Max results (default 50) | [optional] |

### Return type

[**List&lt;NotificationItem&gt;**](NotificationItem.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json, application/problem+json

### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
| **200** | OK |  -  |
| **0** | Error |  -  |

<a id="sendNotification"></a>
# **sendNotification**
> SendOutputBody sendNotification(sendInputBody, xIdempotencyKey)

Send a notification

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.NotificationsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    NotificationsApi apiInstance = new NotificationsApi(defaultClient);
    SendInputBody sendInputBody = new SendInputBody(); // SendInputBody | 
    String xIdempotencyKey = "xIdempotencyKey_example"; // String | Idempotency key for deduplication
    try {
      SendOutputBody result = apiInstance.sendNotification(sendInputBody, xIdempotencyKey);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling NotificationsApi#sendNotification");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters

| Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **sendInputBody** | [**SendInputBody**](SendInputBody.md)|  | |
| **xIdempotencyKey** | **String**| Idempotency key for deduplication | [optional] |

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
| **202** | Accepted |  -  |
| **0** | Error |  -  |

