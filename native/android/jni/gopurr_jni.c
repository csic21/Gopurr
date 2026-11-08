#include <jni.h>
#include <pthread.h>
#include <string.h>
#include "libgopurr.h"

static JavaVM *gopurr_vm;
static jobject vpn_service;
static jmethodID protect_method;
static pthread_mutex_t protector_lock = PTHREAD_MUTEX_INITIALIZER;

JNIEXPORT jint JNICALL JNI_OnLoad(JavaVM *vm, void *reserved) {
    (void)reserved;
    gopurr_vm = vm;
    return JNI_VERSION_1_6;
}

static jstring response_string(JNIEnv *env, char *response) {
    if (!response) return (*env)->NewStringUTF(env, "{\"error\":{\"code\":\"internal\",\"message\":\"The native engine returned no response\"}}");
    // Go returns standard UTF-8, whereas NewStringUTF takes modified UTF-8.
    // Java's byte[] + UTF-8 constructor preserves non-ASCII and emoji names.
    size_t length = strlen(response);
    jbyteArray bytes = (*env)->NewByteArray(env, (jsize)length);
    if (!bytes) { GopurrFree(response); return NULL; }
    (*env)->SetByteArrayRegion(env, bytes, 0, (jsize)length, (jbyte *)response);
    GopurrFree(response);
    jclass string_class = (*env)->FindClass(env, "java/lang/String");
    jmethodID constructor = (*env)->GetMethodID(env, string_class, "<init>", "([BLjava/lang/String;)V");
    jstring encoding = (*env)->NewStringUTF(env, "UTF-8");
    jstring result = (jstring)(*env)->NewObject(env, string_class, constructor, bytes, encoding);
    (*env)->DeleteLocalRef(env, encoding);
    (*env)->DeleteLocalRef(env, string_class);
    (*env)->DeleteLocalRef(env, bytes);
    return result;
}

static char *request_bytes(JNIEnv *env, jstring value) {
    if (!value) return NULL;
    jclass string_class = (*env)->FindClass(env, "java/lang/String");
    jmethodID get_bytes = (*env)->GetMethodID(env, string_class, "getBytes", "(Ljava/lang/String;)[B");
    jstring encoding = (*env)->NewStringUTF(env, "UTF-8");
    jbyteArray bytes = (jbyteArray)(*env)->CallObjectMethod(env, value, get_bytes, encoding);
    (*env)->DeleteLocalRef(env, encoding);
    (*env)->DeleteLocalRef(env, string_class);
    if (!bytes || (*env)->ExceptionCheck(env)) return NULL;
    jsize length = (*env)->GetArrayLength(env, bytes);
    char *result = malloc((size_t)length + 1);
    if (result) {
        (*env)->GetByteArrayRegion(env, bytes, 0, length, (jbyte *)result);
        result[length] = '\0';
    }
    (*env)->DeleteLocalRef(env, bytes);
    return result;
}

static int protect_socket(int fd) {
    JNIEnv *env = NULL;
    int attached = 0;
    jint state = (*gopurr_vm)->GetEnv(gopurr_vm, (void **)&env, JNI_VERSION_1_6);
    if (state == JNI_EDETACHED) {
        if ((*gopurr_vm)->AttachCurrentThread(gopurr_vm, &env, NULL) != JNI_OK) return 0;
        attached = 1;
    } else if (state != JNI_OK) return 0;
    pthread_mutex_lock(&protector_lock);
    jobject service = vpn_service ? (*env)->NewLocalRef(env, vpn_service) : NULL;
    jmethodID method = protect_method;
    pthread_mutex_unlock(&protector_lock);
    int protected = 0;
    if (service && method) {
        protected = (*env)->CallBooleanMethod(env, service, method, (jint)fd) == JNI_TRUE;
        if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); protected = 0; }
        (*env)->DeleteLocalRef(env, service);
    }
    if (attached) (*gopurr_vm)->DetachCurrentThread(gopurr_vm);
    return protected;
}

static void clear_protector(JNIEnv *env) {
    pthread_mutex_lock(&protector_lock);
    if (vpn_service) (*env)->DeleteGlobalRef(env, vpn_service);
    vpn_service = NULL;
    protect_method = NULL;
    pthread_mutex_unlock(&protector_lock);
}

JNIEXPORT jstring JNICALL Java_io_github_gopurr_nativeapp_NativeEngine_nativeOpen(JNIEnv *env, jclass klass, jstring options) {
    (void)klass;
    char *request = request_bytes(env, options);
    if ((*env)->ExceptionCheck(env)) { free(request); return NULL; }
    char *response = GopurrOpen(request);
    free(request);
    return response_string(env, response);
}

JNIEXPORT jstring JNICALL Java_io_github_gopurr_nativeapp_NativeEngine_nativeCall(JNIEnv *env, jclass klass, jlong handle, jstring json) {
    (void)klass;
    char *request = request_bytes(env, json);
    if ((*env)->ExceptionCheck(env)) { free(request); return NULL; }
    char *response = GopurrCall((uint64_t)handle, request);
    free(request);
    return response_string(env, response);
}

JNIEXPORT jstring JNICALL Java_io_github_gopurr_nativeapp_NativeEngine_nativeStartVpn(JNIEnv *env, jclass klass, jlong handle, jint fd, jobject service, jstring plan_token) {
    (void)klass;
    clear_protector(env);
    jclass service_class = (*env)->GetObjectClass(env, service);
    jmethodID method = (*env)->GetMethodID(env, service_class, "protect", "(I)Z");
    (*env)->DeleteLocalRef(env, service_class);
    if (!method) return NULL;
    pthread_mutex_lock(&protector_lock);
    vpn_service = (*env)->NewGlobalRef(env, service);
    protect_method = method;
    pthread_mutex_unlock(&protector_lock);
    char *token = request_bytes(env, plan_token);
    if ((*env)->ExceptionCheck(env)) { free(token); clear_protector(env); return NULL; }
    char *response = GopurrStartVPNWithPlan((uint64_t)handle, fd, (void *)protect_socket, token);
    free(token);
    return response_string(env, response);
}

JNIEXPORT jstring JNICALL Java_io_github_gopurr_nativeapp_NativeEngine_nativeStopVpn(JNIEnv *env, jclass klass, jlong handle) {
    (void)klass;
    char *response = GopurrStopVPN((uint64_t)handle);
    clear_protector(env);
    return response_string(env, response);
}

JNIEXPORT void JNICALL Java_io_github_gopurr_nativeapp_NativeEngine_nativeClose(JNIEnv *env, jclass klass, jlong handle) {
    (void)klass;
    GopurrClose((uint64_t)handle);
    clear_protector(env);
}

JNIEXPORT void JNICALL Java_io_github_gopurr_nativeapp_NativeEngine_nativeCancelPending(JNIEnv *env, jclass klass, jlong handle) {
    (void)env;
    (void)klass;
    GopurrCancelPending((uint64_t)handle);
}
