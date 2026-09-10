package com.renovex.capture.ui.screens

import androidx.compose.foundation.layout.Box
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
import com.renovex.capture.network.ProjectDto
import com.renovex.capture.network.ProjectSpaceApi

/**
 * Design spec/Task 4 UX acceptance: "Projects -> Project -> Spaces ->
 * Space -> Scan Room / AR Review / Concepts." Every list row shows the
 * project's NAME, never its raw ID, as primary navigation.
 */
@Composable
fun ProjectListScreen(
    projectSpaceApi: ProjectSpaceApi,
    onProjectSelected: (id: String, name: String) -> Unit,
) {
    var projects by remember { mutableStateOf<List<ProjectDto>?>(null) }

    LaunchedEffect(Unit) {
        val response = runCatching { projectSpaceApi.listProjects() }.getOrNull()
        projects = response?.takeIf { it.isSuccessful }?.body()?.items ?: emptyList()
    }

    val currentProjects = projects
    if (currentProjects == null) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }

    LazyColumn(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        items(currentProjects) { project ->
            Card(
                modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                onClick = { onProjectSelected(project.id, project.name) },
            ) {
                Text(project.name, modifier = Modifier.padding(16.dp))
            }
        }
    }
}
