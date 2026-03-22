# GroupsApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**createGroup**](GroupsApi.md#createGroup) | **POST** /v1/groups | Create a notification group |
| [**listGroups**](GroupsApi.md#listGroups) | **GET** /v1/groups | List notification groups |
| [**updateGroup**](GroupsApi.md#updateGroup) | **PUT** /v1/groups/{id} | Update a notification group |


<a id="createGroup"></a>
# **createGroup**
> NotificationGroup createGroup(createGroupInputBody)

Create a notification group

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.GroupsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    GroupsApi apiInstance = new GroupsApi(defaultClient);
    CreateGroupInputBody createGroupInputBody = new CreateGroupInputBody(); // CreateGroupInputBody | 
    try {
      NotificationGroup result = apiInstance.createGroup(createGroupInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling GroupsApi#createGroup");
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
| **createGroupInputBody** | [**CreateGroupInputBody**](CreateGroupInputBody.md)|  | |

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
| **201** | Created |  -  |
| **0** | Error |  -  |

<a id="listGroups"></a>
# **listGroups**
> List&lt;NotificationGroup&gt; listGroups()

List notification groups

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.GroupsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    GroupsApi apiInstance = new GroupsApi(defaultClient);
    try {
      List<NotificationGroup> result = apiInstance.listGroups();
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling GroupsApi#listGroups");
      System.err.println("Status code: " + e.getCode());
      System.err.println("Reason: " + e.getResponseBody());
      System.err.println("Response headers: " + e.getResponseHeaders());
      e.printStackTrace();
    }
  }
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**List&lt;NotificationGroup&gt;**](NotificationGroup.md)

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

<a id="updateGroup"></a>
# **updateGroup**
> NotificationGroup updateGroup(id, updateGroupInputBody)

Update a notification group

### Example
```java
// Import classes:
import com.hermes.sdk.ApiClient;
import com.hermes.sdk.ApiException;
import com.hermes.sdk.Configuration;
import com.hermes.sdk.models.*;
import com.hermes.sdk.api.GroupsApi;

public class Example {
  public static void main(String[] args) {
    ApiClient defaultClient = Configuration.getDefaultApiClient();
    defaultClient.setBasePath("http://localhost");

    GroupsApi apiInstance = new GroupsApi(defaultClient);
    String id = "id_example"; // String | Group ID
    UpdateGroupInputBody updateGroupInputBody = new UpdateGroupInputBody(); // UpdateGroupInputBody | 
    try {
      NotificationGroup result = apiInstance.updateGroup(id, updateGroupInputBody);
      System.out.println(result);
    } catch (ApiException e) {
      System.err.println("Exception when calling GroupsApi#updateGroup");
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
| **id** | **String**| Group ID | |
| **updateGroupInputBody** | [**UpdateGroupInputBody**](UpdateGroupInputBody.md)|  | |

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
| **200** | OK |  -  |
| **0** | Error |  -  |

