package com.renovex.capture.ui.screens

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.renovex.capture.network.ProjectSpaceApi
import com.renovex.capture.network.SpaceDto

@Composable
fun SpaceListScreen(
    projectId: String,
    projectName: String,
    projectSpaceApi: ProjectSpaceApi,
    onSpaceSelected: (id: String, name: String) -> Unit,
) {
    var spaces by remember { mutableStateOf<List<SpaceDto>?>(null) }

    LaunchedEffect(projectId) {
        val response = runCatching { projectSpaceApi.listSpaces(projectId) }.getOrNull()
        spaces = response?.takeIf { it.isSuccessful }?.body()?.items ?: emptyList()
    }

    val currentSpaces = spaces
    Column(modifier = Modifier.fillMaxSize()) {
        Text(projectName, modifier = Modifier.padding(16.dp))

        if (currentSpaces == null) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
            return@Column
        }

        if (currentSpaces.isEmpty()) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text("No spaces yet for this project")
            }
            return@Column
        }

        LazyColumn(modifier = Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
            items(currentSpaces) { space ->
                Card(
                    modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                    onClick = { onSpaceSelected(space.id, space.name) },
                ) {
                    Text(space.name, modifier = Modifier.padding(16.dp))
                }
            }
        }
    }
}
